package namespaces

import (
	"testing"

	"github.com/loft-sh/vcluster/pkg/util/translate"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newTranslatorForTest(t *testing.T) translate.Translator {
	oldName := translate.VClusterName
	translate.VClusterName = "tenant"
	t.Cleanup(func() { translate.VClusterName = oldName })
	return NewTranslator("tenant-cp", map[string]string{
		"frontend": "customer-frontend",
		"team-*":   "${name}-team-*",
	})
}

func TestTranslatorHostNamespace(t *testing.T) {
	tr := newTranslatorForTest(t)
	assert.Equal(t, tr.SingleNamespaceTarget(), false)
	assert.Equal(t, tr.HostNamespace(nil, ""), "")
	assert.Equal(t, tr.HostNamespace(nil, "frontend"), "customer-frontend")
	assert.Equal(t, tr.HostNamespace(nil, "team-dev"), "tenant-team-dev")
	assert.Equal(t, tr.HostNamespace(nil, "other"), "tenant-cp")

	assert.DeepEqual(t, tr.HostName(nil, "nginx", "team-dev"), types.NamespacedName{Name: "nginx", Namespace: "tenant-team-dev"})
	assert.DeepEqual(t, tr.HostNameShort(nil, "nginx", "team-dev"), types.NamespacedName{Name: "nginx", Namespace: "tenant-team-dev"})
	assert.DeepEqual(t, tr.HostName(nil, "nginx", "other"), types.NamespacedName{Name: "nginx-x-other-x-tenant", Namespace: "tenant-cp"})
	assert.Equal(t, tr.HostNameShort(nil, "nginx", "other").Namespace, "tenant-cp")
	assert.DeepEqual(t, tr.HostName(nil, "", "team-dev"), types.NamespacedName{})
	assert.Equal(t, tr.MarkerLabelCluster(), "tenant-cp-x-tenant")
}

func TestTranslatorIsTargetedNamespace(t *testing.T) {
	tr := newTranslatorForTest(t)
	assert.Assert(t, tr.IsTargetedNamespace(nil, "tenant-cp"))
	assert.Assert(t, tr.IsTargetedNamespace(nil, "customer-frontend"))
	assert.Assert(t, tr.IsTargetedNamespace(nil, "tenant-team-dev"))
	assert.Assert(t, !tr.IsTargetedNamespace(nil, "kube-system"))
	assert.Assert(t, !tr.IsTargetedNamespace(nil, "other-tenant-team-dev"))
}

func TestTranslatorIsManaged(t *testing.T) {
	tr := newTranslatorForTest(t)
	kind := corev1.SchemeGroupVersion.WithKind("Secret").String()

	managed := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: "nginx", Namespace: "tenant-team-dev",
		Labels: map[string]string{translate.MarkerLabel: "tenant"},
		Annotations: map[string]string{
			translate.NameAnnotation: "nginx", translate.NamespaceAnnotation: "team-dev", translate.KindAnnotation: kind,
			translate.HostNameAnnotation: "nginx", translate.HostNamespaceAnnotation: "tenant-team-dev",
		},
	}}
	assert.Assert(t, tr.IsManaged(nil, managed))

	preExisting := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: "tenant-team-dev"}}
	assert.Assert(t, !tr.IsManaged(nil, preExisting))

	otherVCluster := managed.DeepCopy()
	otherVCluster.Labels[translate.MarkerLabel] = "someone-else"
	assert.Assert(t, !tr.IsManaged(nil, otherVCluster))

	wrongKind := managed.DeepCopy()
	wrongKind.Annotations[translate.KindAnnotation] = corev1.SchemeGroupVersion.WithKind("ConfigMap").String()
	assert.Assert(t, !tr.IsManaged(nil, wrongKind))

	untargeted := managed.DeepCopy()
	untargeted.Namespace = "kube-system"
	assert.Assert(t, !tr.IsManaged(nil, untargeted))

	clusterScoped := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "tenant-team-dev",
		Labels: map[string]string{translate.MarkerLabel: tr.MarkerLabelCluster()},
	}}
	assert.Assert(t, tr.IsManaged(nil, clusterScoped))
}
