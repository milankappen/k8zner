//go:build integration

package controller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8znerv1alpha1 "github.com/milankappen/k8zner/api/v1alpha1"
)

// These tests exercise the CRD schema served by the envtest apiserver
// (installed from config/crd/bases), so they cover the validation rules end
// to end: enums, patterns, and CEL immutability rules.
var _ = Describe("K8znerCluster CRD validation", func() {
	newValidCluster := func(name string) *k8znerv1alpha1.K8znerCluster {
		return &k8znerv1alpha1.K8znerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: k8znerv1alpha1.K8znerClusterSpec{
				Region:        "fsn1",
				ControlPlanes: k8znerv1alpha1.ControlPlaneSpec{Count: 1, Size: "cx22"},
				Workers:       k8znerv1alpha1.WorkerSpec{Count: 1, Size: "cx22"},
				Kubernetes:    k8znerv1alpha1.KubernetesSpec{Version: "1.32.0"},
				Talos:         k8znerv1alpha1.TalosSpec{Version: "v1.10.0"},
				Paused:        true, // keep the reconciler out of these tests
			},
		}
	}

	var counter int
	var cluster *k8znerv1alpha1.K8znerCluster

	BeforeEach(func() {
		counter++
		cluster = newValidCluster(fmt.Sprintf("crd-validation-%d-%d", GinkgoRandomSeed(), counter))
	})

	AfterEach(func() {
		_ = k8sClient.Delete(ctx, cluster)
	})

	It("rejects an invalid domain", func() {
		cluster.Spec.Domain = "Not_A_Domain!"
		Expect(k8sClient.Create(ctx, cluster)).NotTo(Succeed())
	})

	It("rejects an invalid subdomain", func() {
		cluster.Spec.Domain = "example.com"
		cluster.Spec.Addons = &k8znerv1alpha1.AddonSpec{ArgoSubdomain: "bad.subdomain"}
		Expect(k8sClient.Create(ctx, cluster)).NotTo(Succeed())
	})

	It("accepts a valid domain and subdomain", func() {
		cluster.Spec.Domain = "example.com"
		cluster.Spec.Addons = &k8znerv1alpha1.AddonSpec{ArgoSubdomain: "argo-test"}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
	})

	It("rejects changing the region after creation", func() {
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		cluster.Spec.Region = "nbg1"
		err := k8sClient.Update(ctx, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("region is immutable"))
	})

	It("allows setting credentialsRef when previously empty, then freezes it", func() {
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		cluster.Spec.CredentialsRef.Name = "my-credentials"
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

		cluster.Spec.CredentialsRef.Name = "other-credentials"
		err := k8sClient.Update(ctx, cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("credentialsRef is immutable"))
	})
})
