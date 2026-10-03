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

// Package v1 定义 db.com/v1 组下的 Database 自定义资源。
//
// GVR：db.com/v1，Resource=dbs（shortName=db），Kind=Database。
package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion 标识本包资源的 group/version。
	GroupVersion = schema.GroupVersion{Group: "db.com", Version: "v1"}

	// Resource 是 Database 的复数资源名，即 GVR 中的 R。
	Resource = schema.GroupVersionResource{Group: "db.com", Version: "v1", Resource: "dbs"}

	// SchemeBuilder 用于把本包类型注册进 scheme。
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme 把本包类型加入指定 scheme。
	AddToScheme = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&Database{}, &DatabaseList{})
}

// +kubebuilder:object:generate=true
// +groupName=db.com
