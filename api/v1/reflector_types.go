/*
Copyright 2024.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReflectorSpec defines the desired state of Reflector
type ReflectorSpec struct {
	// Selector defines the label selector for pods.
	// It must match the labels of the pod template.
	Selector *metav1.LabelSelector `json:"selector" protobuf:"bytes,1,opt,name=selector"`

	// Template describes the pods that will be created.
	Template corev1.PodTemplateSpec `json:"template" protobuf:"bytes,2,opt,name=template"`
}

// ReflectorStatus defines the observed state of Reflector
type ReflectorStatus struct {
	// Add additional status fields here.
	// Example: "Number of active pods"
	ActivePods int32 `json:"activePods,omitempty" protobuf:"varint,1,opt,name=activePods"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Active Pods",type="integer",JSONPath=".status.activePods",description="The number of active pods"

// Reflector is the Schema for the reflectors API
type Reflector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   ReflectorSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status ReflectorStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

//+kubebuilder:object:root=true

// ReflectorList contains a list of Reflector
type ReflectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Reflector `json:"items" protobuf:"bytes,2,rep,name=items"`
}

func init() {
	SchemeBuilder.Register(&Reflector{}, &ReflectorList{})
}
