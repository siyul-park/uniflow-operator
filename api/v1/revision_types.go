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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RevisionTemplateSpec describes the data a revision should have when created from a template.
type RevisionTemplateSpec struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec RevisionSpec `json:"spec,omitempty"`
}

// RevisionSpec defines the desired state of a Revision.
type RevisionSpec struct {
	// Embeds the PodSpec to inherit container specifications.
	corev1.PodSpec `json:",inline"`

	// WatchProbe specifies the probe configuration for monitoring the revision.
	// This is not allowed for ephemeral containers.
	// +optional
	WatchProbe *corev1.Probe `json:"watchProbe,omitempty"`
}

// RevisionStatus defines the observed state of Revision
type RevisionStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Revision is the Schema for the revisions API
type Revision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RevisionSpec   `json:"spec,omitempty"`
	Status RevisionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RevisionList contains a list of Revision
type RevisionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Revision `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Revision{}, &RevisionList{})
}
