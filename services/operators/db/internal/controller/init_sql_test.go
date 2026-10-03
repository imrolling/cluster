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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dbv1 "github.com/personal/cluster/services/operators/db/api/v1"
)

func TestRenderInitSQL_Basic(t *testing.T) {
	dbs := []dbv1.InitDatabase{
		{
			Name: "biz_order",
			Tables: []dbv1.InitTable{
				{
					Name:       "t_order",
					Comment:    "订单主表",
					PrimaryKey: "id",
					Columns: []dbv1.InitColumn{
						{Name: "id", Type: "BIGINT AUTO_INCREMENT", Comment: "主键"},
						{Name: "order_no", Type: "VARCHAR(64)", Comment: "订单号"},
						{Name: "amount", Type: "DECIMAL(12,2)", Nullable: true, Comment: "金额"},
					},
				},
			},
		},
	}
	sql, err := RenderInitSQL(dbs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"CREATE DATABASE IF NOT EXISTS `biz_order` DEFAULT CHARACTER SET utf8mb4;",
		"USE `biz_order`;",
		"CREATE TABLE IF NOT EXISTS `t_order` (",
		"`id` BIGINT AUTO_INCREMENT NOT NULL COMMENT '主键'",
		"PRIMARY KEY (`id`)",
		"COMMENT='订单主表'",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("init SQL missing %q\n got: %s", want, sql)
		}
	}
}

func TestRenderInitSQL_Empty(t *testing.T) {
	if sql, err := RenderInitSQL(nil); err != nil || sql != "" {
		t.Fatalf("empty databases should render empty SQL, got %q, err %v", sql, err)
	}
}

func TestRenderInitSQL_InvalidIdentifier(t *testing.T) {
	dbs := []dbv1.InitDatabase{
		{Name: "bad-name; DROP TABLE x", Tables: nil},
	}
	if _, err := RenderInitSQL(dbs); err == nil {
		t.Fatal("expected error for invalid identifier, got nil")
	}
}

func TestRenderInitSQL_InvalidType(t *testing.T) {
	dbs := []dbv1.InitDatabase{
		{Name: "db1", Tables: []dbv1.InitTable{{
			Name:    "t1",
			Columns: []dbv1.InitColumn{{Name: "c1", Type: "VARCHAR(64); DROP TABLE t1"}},
		}}},
	}
	if _, err := RenderInitSQL(dbs); err == nil {
		t.Fatal("expected error for invalid column type, got nil")
	}
}

func TestRenderInitSQL_AutoIncrementRequiresPK(t *testing.T) {
	dbs := []dbv1.InitDatabase{
		{Name: "db1", Tables: []dbv1.InitTable{{
			Name:    "t1",
			Columns: []dbv1.InitColumn{{Name: "id", Type: "BIGINT AUTO_INCREMENT"}},
		}}},
	}
	if _, err := RenderInitSQL(dbs); err == nil {
		t.Fatal("expected error for AUTO_INCREMENT without primaryKey, got nil")
	}
}

func TestRenderInitSQL_PrimaryKeyNotInColumns(t *testing.T) {
	dbs := []dbv1.InitDatabase{
		{Name: "db1", Tables: []dbv1.InitTable{{
			Name:       "t1",
			PrimaryKey: "id",
			Columns:    []dbv1.InitColumn{{Name: "c1", Type: "VARCHAR(64)"}},
		}}},
	}
	if _, err := RenderInitSQL(dbs); err == nil {
		t.Fatal("expected error for primaryKey not in columns, got nil")
	}
}

func TestDesiredStatefulSet_Defaults(t *testing.T) {
	db := &dbv1.Database{
		ObjectMeta:   metav1.ObjectMeta{Name: "demo", Namespace: "ns1"},
		Spec:         dbv1.DatabaseSpec{RootPassword: "x"},
	}
	sts := desiredStatefulSet(db, "demo-secret", "demo-config", true)
	if got := *sts.Spec.Replicas; got != 1 {
		t.Errorf("default replicas = %d, want 1", got)
	}
	if sts.Spec.ServiceName != "demo-headless" {
		t.Errorf("ServiceName = %q, want demo-headless", sts.Spec.ServiceName)
	}
	if sts.Spec.Template.Spec.Containers[0].Image != "mysql:8.0" {
		t.Errorf("image = %q, want mysql:8.0", sts.Spec.Template.Spec.Containers[0].Image)
	}
	// 校验挂载点：my.cnf 与 init.sql
	mounts := sts.Spec.Template.Spec.Containers[0].VolumeMounts
	if len(mounts) != 2 {
		t.Fatalf("mounts len = %d, want 2", len(mounts))
	}
	if mounts[0].MountPath != "/etc/mysql/conf.d/my.cnf" || mounts[0].SubPath != "my.cnf" {
		t.Errorf("my.cnf mount wrong: %+v", mounts[0])
	}
	if mounts[1].MountPath != "/docker-entrypoint-initdb.d/init.sql" || mounts[1].SubPath != "init.sql" {
		t.Errorf("init.sql mount wrong: %+v", mounts[1])
	}
	// 密码经 Secret 引用
	env := sts.Spec.Template.Spec.Containers[0].Env
	if len(env) != 1 || env[0].Name != "MYSQL_ROOT_PASSWORD" || env[0].ValueFrom == nil ||
		env[0].ValueFrom.SecretKeyRef.Name != "demo-secret" {
		t.Errorf("MYSQL_ROOT_PASSWORD env wrong: %+v", env)
	}
	// PVC 模板默认 10Gi
	vct := sts.Spec.VolumeClaimTemplates[0]
	if vct.Spec.Resources.Requests.Storage().String() != "10Gi" {
		t.Errorf("pvc size = %s, want 10Gi", vct.Spec.Resources.Requests.Storage().String())
	}
}

func TestDesiredConfigData_CustomConfig(t *testing.T) {
	db := &dbv1.Database{Spec: dbv1.DatabaseSpec{Config: "[mysqld]\nmax_connections=500\n"}}
	data := desiredConfigData(db, "")
	if data["my.cnf"] != db.Spec.Config {
		t.Errorf("custom my.cnf not used: %q", data["my.cnf"])
	}
	if _, ok := data["init.sql"]; ok {
		t.Error("init.sql should be absent when no databases defined")
	}
}
