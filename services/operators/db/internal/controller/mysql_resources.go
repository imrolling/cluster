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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	dbv1 "github.com/personal/cluster/services/operators/db/api/v1"
)

const (
	defaultMySQLImage   = "mysql:8.0"
	defaultMySQLPort    = int32(3306)
	defaultStorageSize  = "10Gi"
	rootPasswordKey     = "root-password"
	mysqlPortName       = "mysql"
	headlessSvcSuffix   = "-headless"
	secretSuffix        = "-secret"
	configMapSuffix     = "-config"
	initDBMountDir      = "/docker-entrypoint-initdb.d/init.sql"
	myCnfMountPath      = "/etc/mysql/conf.d/my.cnf"
	terminationGraceSec = int64(60)
)


const defaultMySQLConfig = `[mysqld]
character-set-server = utf8mb4
collation-server = utf8mb4_unicode_ci
skip-name-resolve
`

func childLabels(db *dbv1.Database) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "db-operator",
		"app.kubernetes.io/name":       "mysql",
		"app.kubernetes.io/instance":   db.Name,
		"db.com/database":              db.Name,
	}
}

func objectMeta(db *dbv1.Database, suffix string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      db.Name + suffix,
		Namespace: db.Namespace,
		Labels:    childLabels(db),
	}
}

func replicasFor(db *dbv1.Database) int32 {
	if db.Spec.Replicas <= 0 {
		return 1
	}
	return db.Spec.Replicas
}

func imageFor(db *dbv1.Database) string {
	if db.Spec.Image != "" {
		return db.Spec.Image
	}
	return defaultMySQLImage
}

func portFor(db *dbv1.Database) int32 {
	if db.Spec.Port > 0 {
		return db.Spec.Port
	}
	return defaultMySQLPort
}

func storageSizeFor(db *dbv1.Database) resource.Quantity {
	if db.Spec.Storage.Size.IsZero() {
		return resource.MustParse(defaultStorageSize)
	}
	return db.Spec.Storage.Size
}

func mycnfFor(db *dbv1.Database) string {
	if db.Spec.Config != "" {
		// 用户整段覆盖默认 my.cnf
		return db.Spec.Config
	}
	return defaultMySQLConfig
}

// desiredSecret 生成 root 密码 Secret（CR 明文密码落 Secret，Pod 经 secretKeyRef 引用）。
func desiredSecret(db *dbv1.Database) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: objectMeta(db, secretSuffix),
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			rootPasswordKey: []byte(db.Spec.RootPassword),
		},
	}
}

// desiredConfigData 生成 ConfigMap 数据：my.cnf（挂载 /etc/mysql/conf.d）+ init.sql（首次初始化脚本）。
func desiredConfigData(db *dbv1.Database, initSQL string) map[string]string {
	data := map[string]string{"my.cnf": mycnfFor(db)}
	if initSQL != "" {
		data["init.sql"] = initSQL
	}
	return data
}

func desiredConfigMap(db *dbv1.Database, initSQL string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: objectMeta(db, configMapSuffix),
		Data:       desiredConfigData(db, initSQL),
	}
}

func desiredServices(db *dbv1.Database) (headless, clientSvc *corev1.Service) {
	labels := childLabels(db)
	port := portFor(db)
	ports := []corev1.ServicePort{
		{Name: mysqlPortName, Port: port, TargetPort: intstr.FromString(mysqlPortName)},
	}
	headless = &corev1.Service{
		ObjectMeta: objectMeta(db, headlessSvcSuffix),
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  labels,
			Ports:     ports,
		},
	}
	clientSvc = &corev1.Service{
		ObjectMeta: objectMeta(db, ""),
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports:    ports,
		},
	}
	return headless, clientSvc
}

// desiredStatefulSet 生成 MySQL StatefulSet：
//   - 密码经 Secret 注入 MYSQL_ROOT_PASSWORD；
//   - my.cnf 经 ConfigMap 挂载到 /etc/mysql/conf.d/my.cnf；
//   - init.sql 经 ConfigMap 挂载到 /docker-entrypoint-initdb.d/init.sql（仅首次初始化执行）；
//   - 数据盘由 volumeClaimTemplates(data) 提供，容量/存储类来自 spec.storage。
func desiredStatefulSet(db *dbv1.Database, secretName, configMapName string, hasInitSQL bool) *appsv1.StatefulSet {
	labels := childLabels(db)
	port := portFor(db)

	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: myCnfMountPath, SubPath: "my.cnf", ReadOnly: true},
	}
	volumes := []corev1.Volume{{
		Name: "config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
				Items:                []corev1.KeyToPath{{Key: "my.cnf", Path: "my.cnf"}},
			},
		},
	}}
	if hasInitSQL {
		mounts = append(mounts, corev1.VolumeMount{
			Name: "initdb", MountPath: initDBMountDir, SubPath: "init.sql", ReadOnly: true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: "initdb",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
					Items:                []corev1.KeyToPath{{Key: "init.sql", Path: "init.sql"}},
				},
			},
		})
	}

	container := corev1.Container{
		Name:    "mysql",
		Image:   imageFor(db),
		Ports:   []corev1.ContainerPort{{Name: mysqlPortName, ContainerPort: port}},
		Env: []corev1.EnvVar{{
			Name: "MYSQL_ROOT_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  rootPasswordKey,
				},
			},
		}},
		VolumeMounts: mounts,
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{Command: []string{
					"sh", "-c", `mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD"`,
				}},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
			TimeoutSeconds:      5,
			FailureThreshold:    12,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{Command: []string{
					"sh", "-c", `mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD"`,
				}},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    6,
		},
	}
	if db.Spec.Resources != nil {
		container.Resources = *db.Spec.Resources.DeepCopy()
	}

	replicas := replicasFor(db)
	tgs := terminationGraceSec
	sts := &appsv1.StatefulSet{
		ObjectMeta: objectMeta(db, ""),
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: db.Name + headlessSvcSuffix,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers:                    []corev1.Container{container},
					Volumes:                       volumes,
					TerminationGracePeriodSeconds: &tgs,
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data"},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: storageSizeFor(db)},
					},
					StorageClassName: db.Spec.Storage.ClassName,
				},
			}},
		},
	}
	return sts
}
