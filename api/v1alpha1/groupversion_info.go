// Package v1alpha1 contains API Schema definitions for the k8zner.io v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=k8zner.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "k8zner.io", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	// Plain apimachinery rather than controller-runtime's deprecated
	// scheme.Builder: api packages must keep minimal dependencies.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme
	AddToScheme = SchemeBuilder.AddToScheme

	// Scheme is the runtime scheme containing the registered types
	Scheme = runtime.NewScheme()
)

// addKnownTypes registers this group's types with a scheme.
func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &K8znerCluster{}, &K8znerClusterList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}

func init() {
	// Add core Kubernetes types to the Scheme (for Namespace, Secret, etc.)
	_ = clientgoscheme.AddToScheme(Scheme)

	// Add our types to the Scheme
	_ = AddToScheme(Scheme)
}
