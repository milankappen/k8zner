//go:build integration

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/milankappen/k8zner/internal/operator/crds"
)

// The envtest apiserver already has the CRD installed from config/crd/bases,
// so this exercises the startup self-apply path against a server where the
// CRD exists — the situation every operator restart hits.
var _ = Describe("CRD self-apply", func() {
	It("server-side-applies the embedded CRD idempotently", func() {
		s := scheme.Scheme
		Expect(apiextensionsv1.AddToScheme(s)).To(Succeed())

		directClient, err := client.New(cfg, client.Options{Scheme: s})
		Expect(err).NotTo(HaveOccurred())

		// Applying twice must not conflict: startup runs on every restart.
		Expect(crds.Ensure(ctx, directClient)).To(Succeed())
		Expect(crds.Ensure(ctx, directClient)).To(Succeed())

		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(directClient.Get(ctx, types.NamespacedName{Name: "k8znerclusters.k8zner.io"}, crd)).To(Succeed())

		owners := make([]string, 0, len(crd.ManagedFields))
		for _, mf := range crd.ManagedFields {
			owners = append(owners, mf.Manager)
		}
		Expect(owners).To(ContainElement("k8zner-operator"))
	})
})
