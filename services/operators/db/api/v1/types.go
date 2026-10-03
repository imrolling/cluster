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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DatabaseSpec 定义 Database CR 的期望状态。
type DatabaseSpec struct {
	// Replicas 期望的 MySQL Pod 副本数（默认 1；>1 时为多个独立 MySQL 实例，无主从复制）
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=7
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Image MySQL 镜像（默认 mysql:8.0）
	// +kubebuilder:default="mysql:8.0"
	// +optional
	Image string `json:"image,omitempty"`

	// Port 服务端口（默认 3306）
	// +kubebuilder:default=3306
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// RootPassword root 密码（CR 明文定义，operator 落入 Secret 供 Pod 引用；
	// 注意：镜像仅在数据目录首次初始化时读取该密码，初始化后修改需重建实例才生效）
	// +kubebuilder:validation:MinLength=1
	RootPassword string `json:"rootPassword"`

	// Config 自定义 my.cnf 配置内容（整段覆盖默认配置），经 ConfigMap 挂载到 /etc/mysql/conf.d/my.cnf
	// +optional
	Config string `json:"config,omitempty"`

	// Storage 数据盘配置（默认 10Gi）
	// +optional
	Storage DatabaseStorage `json:"storage,omitempty"`

	// Resources MySQL 容器资源 requests/limits
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Databases 需自动化初始化的数据库及数据表（渲染为 init.sql，仅首次初始化生效）
	// +optional
	Databases []InitDatabase `json:"databases,omitempty"`
}

// DatabaseStorage 数据盘配置。
type DatabaseStorage struct {
	// Size 数据盘容量（默认 10Gi）
	// +kubebuilder:default="10Gi"
	// +optional
	Size resource.Quantity `json:"size,omitempty"`

	// ClassName 存储类名称（留空使用集群默认 StorageClass）
	// +optional
	ClassName *string `json:"storageClassName,omitempty"`
}

// InitDatabase 待自动化初始化的数据库。
type InitDatabase struct {
	// Name 数据库名（字母开头，字母/数字/下划线，≤64 字符）
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9_]{0,63}$`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Charset 字符集（默认 utf8mb4）
	// +kubebuilder:default="utf8mb4"
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_]+$`
	// +optional
	Charset string `json:"charset,omitempty"`

	// Tables 需初始化的数据表
	// +optional
	Tables []InitTable `json:"tables,omitempty"`
}

// InitTable 待初始化的数据表。
type InitTable struct {
	// Name 表名（字母开头，字母/数字/下划线，≤64 字符）
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9_]{0,63}$`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Comment 表注释
	// +optional
	Comment string `json:"comment,omitempty"`

	// Columns 列定义
	// +kubebuilder:validation:MinItems=1
	Columns []InitColumn `json:"columns"`

	// PrimaryKey 主键列名（须为 columns 中已定义的列；含 AUTO_INCREMENT 列时必填）
	// +optional
	PrimaryKey string `json:"primaryKey,omitempty"`
}

// InitColumn 列定义。
type InitColumn struct {
	// Name 列名（字母开头，字母/数字/下划线，≤64 字符）
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9_]{0,63}$`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Type SQL 数据类型，如 VARCHAR(64)、BIGINT AUTO_INCREMENT、DECIMAL(12,2)
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9_ (),]*$`
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type"`

	// Nullable 是否允许 NULL（默认 NOT NULL）
	// +optional
	Nullable bool `json:"nullable,omitempty"`

	// Comment 列注释
	// +optional
	Comment string `json:"comment,omitempty"`
}

// DatabaseStatus Database CR 的观测状态。
type DatabaseStatus struct {
	// ObservedGeneration 最近一次 reconcile 处理的 generation
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ReadyReplicas 就绪的 MySQL Pod 副本数
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Phase 总体阶段：Ready / Progressing / Error
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions 状态条件
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=dbs,singular=db,shortName=db
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Database 是 db-operator 的核心资源：一份 CR 声明即一套 MySQL 实例（StatefulSet 承载）。
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63",message="metadata.name 需 ≤63 字符（子资源 StatefulSet/Service 名称直接复用 CR 名）"
type Database struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseSpec   `json:"spec,omitempty"`
	Status DatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseList 是 Database 的列表。
type DatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Database `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Database{}, &DatabaseList{})
}
