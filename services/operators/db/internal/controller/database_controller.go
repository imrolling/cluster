/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dbv1 "github.com/personal/cluster/services/operators/db/api/v1"
)

const (
	condReadyType     = "Ready"
	phaseReady        = "Ready"
	phaseProgressing  = "Progressing"
	phaseError        = "Error"
	reasonReconciled  = "ResourcesReady"
	reasonProvision   = "Provisioning"
	reasonReconcileEr = "ReconcileError"
	conditionMsgLimit = 512
)

// DatabaseReconciler 调谐 Database CR：同步 Secret/ConfigMap/Service/StatefulSet 并回写状态。
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile 主调谐入口。
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var db dbv1.Database
	if err := r.Get(ctx, req.NamespacedName, &db); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Database 已被删除，跳过", "name", req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 删除中：交给 GC 级联清理
	if !db.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	log.Info("reconciling Database", "name", db.Name, "generation", db.Generation)

	ready, err := r.syncAll(ctx, &db)
	if err != nil {
		log.Error(err, "sync resources failed")
		if uerr := r.updateStatus(ctx, &db, 0, phaseError, reasonReconcileEr, err.Error()); uerr != nil {
			log.Error(uerr, "更新错误状态失败")
		}
		return ctrl.Result{}, err
	}

	desired := replicasFor(&db)
	if ready == desired {
		if err := r.updateStatus(ctx, &db, ready, phaseReady, reasonReconciled,
			fmt.Sprintf("MySQL 实例就绪：%d/%d 副本", ready, desired)); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.updateStatus(ctx, &db, ready, phaseProgressing, reasonProvision,
			fmt.Sprintf("StatefulSet 就绪 %d/%d 副本", ready, desired)); err != nil {
			return ctrl.Result{}, err
		}
		// 未就绪时 30s 后重试
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// syncAll 同步全部子资源，返回 STS 当前就绪副本数。
func (r *DatabaseReconciler) syncAll(ctx context.Context, db *dbv1.Database) (int32, error) {
	if db.Spec.RootPassword == "" {
		return 0, fmt.Errorf("spec.rootPassword 不能为空")
	}

	initSQL, err := RenderInitSQL(db.Spec.Databases)
	if err != nil {
		return 0, fmt.Errorf("init SQL 校验/渲染失败: %w", err)
	}

	secret := desiredSecret(db)
	if err := r.createOrUpdate(ctx, db, secret, func() error {
		secret.Data = map[string][]byte{rootPasswordKey: []byte(db.Spec.RootPassword)}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("同步 Secret 失败: %w", err)
	}

	cm := desiredConfigMap(db, initSQL)
	if err := r.createOrUpdate(ctx, db, cm, func() error {
		cm.Data = desiredConfigData(db, initSQL)
		return nil
	}); err != nil {
		return 0, fmt.Errorf("同步 ConfigMap 失败: %w", err)
	}

	headless, clientSvc := desiredServices(db)
	if err := r.createOrUpdate(ctx, db, headless, func() error {
		_, want := desiredServices(db)
		headless.Spec = want.Spec
		return nil
	}); err != nil {
		return 0, fmt.Errorf("同步 headless Service 失败: %w", err)
	}
	if err := r.createOrUpdate(ctx, db, clientSvc, func() error {
		_, want := desiredServices(db)
		clientSvc.Spec = want.Spec
		return nil
	}); err != nil {
		return 0, fmt.Errorf("同步 Service 失败: %w", err)
	}

	sts := desiredStatefulSet(db, secret.Name, cm.Name, initSQL != "")
	if err := r.createOrUpdate(ctx, db, sts, func() error {
		want := desiredStatefulSet(db, secret.Name, cm.Name, initSQL != "")
		sts.Spec = want.Spec
		sts.Labels = want.Labels
		return nil
	}); err != nil {
		return 0, fmt.Errorf("同步 StatefulSet 失败: %w", err)
	}

	// 读取 STS 就绪副本数，用于状态回写
	var current appsv1.StatefulSet
	if err := r.Get(ctx, client.ObjectKeyFromObject(sts), &current); err != nil {
		if errors.IsNotFound(err) {
			return 0, nil // 刚创建，本轮视为 0 就绪
		}
		return 0, fmt.Errorf("查询 StatefulSet 状态失败: %w", err)
	}
	return current.Status.ReadyReplicas, nil
}

// createOrUpdate 带 OwnerReference 的 CreateOrUpdate 封装。
func (r *DatabaseReconciler) createOrUpdate(ctx context.Context, owner *dbv1.Database, obj client.Object, mutate controllerutil.MutateFn) error {
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
			return err
		}
		return mutate()
	})
	return err
}

// updateStatus 回写 CR 状态（含 Ready 条件）；内容未变化时跳过写操作。
func (r *DatabaseReconciler) updateStatus(ctx context.Context, db *dbv1.Database, ready int32, phase, reason, message string) error {
	cond := metav1.Condition{
		Type:               condReadyType,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            truncateMessage(message, conditionMsgLimit),
		ObservedGeneration: db.Generation,
		LastTransitionTime: metav1.Now(),
	}
	if phase != phaseReady {
		cond.Status = metav1.ConditionFalse
	}

	var fresh dbv1.Database
	if err := r.Get(ctx, client.ObjectKeyFromObject(db), &fresh); err != nil {
		return client.IgnoreNotFound(err)
	}
	if statusEqual(&fresh.Status, db.Generation, ready, phase, cond) {
		return nil
	}
	fresh.Status.ObservedGeneration = db.Generation
	fresh.Status.ReadyReplicas = ready
	fresh.Status.Phase = phase
	meta.SetStatusCondition(&fresh.Status.Conditions, cond)
	return r.Status().Update(ctx, &fresh)
}

func statusEqual(st *dbv1.DatabaseStatus, gen int64, ready int32, phase string, cond metav1.Condition) bool {
	if st.ObservedGeneration != gen || st.ReadyReplicas != ready || st.Phase != phase {
		return false
	}
	cur := meta.FindStatusCondition(st.Conditions, cond.Type)
	if cur == nil {
		return false
	}
	return cur.Status == cond.Status && cur.Reason == cond.Reason && cur.Message == cond.Message
}

func truncateMessage(msg string, max int) string {
	runes := []rune(msg)
	if len(runes) <= max {
		return msg
	}
	return string(runes[:max])
}

// SetupWithManager 注册控制器与 watch 的从属资源。
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dbv1.Database{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
