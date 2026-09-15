package namespaces

import (
	"context"
	"testing"

	"github.com/loft-sh/vcluster/pkg/mappings"
	"github.com/loft-sh/vcluster/pkg/mappings/resources"
	"github.com/loft-sh/vcluster/pkg/mappings/store"
	"github.com/loft-sh/vcluster/pkg/scheme"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	testingutil "github.com/loft-sh/vcluster/pkg/util/testing"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// newMapperForTest goes through resources.CreateNamespacesMapper so the pro hook override registered in init() is
// exercised as well.
func newMapperForTest(t *testing.T) (*synccontext.SyncContext, synccontext.Mapper) {
	storeBackend := store.NewMemoryBackend()
	mappingsStore, err := store.NewStore(context.TODO(), nil, nil, storeBackend)
	assert.NilError(t, err)

	vConfig := testingutil.NewFakeConfig()
	vConfig.Name = "tenant"
	vConfig.HostNamespace = "tenant-cp"
	vConfig.Sync.ToHost.Namespaces.Enabled = true
	vConfig.Sync.ToHost.Namespaces.Mappings.ByName = map[string]string{
		"frontend": "customer-frontend",
		"team-*":   "${name}-team-*",
	}

	oldName, oldDefault := translate.VClusterName, translate.Default
	t.Cleanup(func() { translate.VClusterName, translate.Default = oldName, oldDefault })
	translate.VClusterName = "tenant"
	translate.Default = NewTranslator("tenant-cp", vConfig.Sync.ToHost.Namespaces.Mappings.ByName)

	mappingsRegistry := mappings.NewMappingsRegistry(mappingsStore)
	registerContext := &synccontext.RegisterContext{
		Context:        context.TODO(),
		Config:         vConfig,
		Mappings:       mappingsRegistry,
		HostManager:    testingutil.NewFakeManager(testingutil.NewFakeClient(scheme.Scheme)),
		VirtualManager: testingutil.NewFakeManager(testingutil.NewFakeClient(scheme.Scheme)),
	}

	namespaceMapper, err := resources.CreateNamespacesMapper(registerContext)
	assert.NilError(t, err)
	assert.NilError(t, mappingsRegistry.AddMapper(namespaceMapper))

	return registerContext.ToSyncContext("test"), namespaceMapper
}

func TestMapperVirtualToHost(t *testing.T) {
	syncCtx, m := newMapperForTest(t)
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: "frontend"}, nil), types.NamespacedName{Name: "customer-frontend"})
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: "team-dev"}, nil), types.NamespacedName{Name: "tenant-team-dev"})
	// unmapped namespaces resolve to the control plane namespace so lookups like the DNS service work
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: "kube-system"}, nil), types.NamespacedName{Name: "tenant-cp"})
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: ""}, nil), types.NamespacedName{})
}

func TestMapperHostToVirtual(t *testing.T) {
	syncCtx, m := newMapperForTest(t)
	assert.DeepEqual(t, m.HostToVirtual(syncCtx, types.NamespacedName{Name: "customer-frontend"}, nil), types.NamespacedName{Name: "frontend"})
	assert.DeepEqual(t, m.HostToVirtual(syncCtx, types.NamespacedName{Name: "tenant-team-dev"}, nil), types.NamespacedName{Name: "team-dev"})
	assert.DeepEqual(t, m.HostToVirtual(syncCtx, types.NamespacedName{Name: "tenant-cp"}, nil), types.NamespacedName{})
	assert.DeepEqual(t, m.HostToVirtual(syncCtx, types.NamespacedName{Name: "kube-system"}, nil), types.NamespacedName{})
}

func TestMapperIsManaged(t *testing.T) {
	syncCtx, m := newMapperForTest(t)

	check := func(ns *corev1.Namespace) bool {
		managed, err := m.IsManaged(syncCtx, ns)
		assert.NilError(t, err)
		return managed
	}

	assert.Assert(t, check(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-team-dev"}}), "pre-existing mapped namespace is imported")
	assert.Assert(t, check(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "tenant-team-dev",
		Labels: map[string]string{translate.MarkerLabel: "tenant-cp-x-tenant"},
	}}), "namespace created by this vcluster")
	assert.Assert(t, !check(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "tenant-team-dev",
		Labels: map[string]string{translate.MarkerLabel: "other-cp-x-other"},
	}}), "namespace owned by another vcluster")
	assert.Assert(t, !check(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-cp"}}), "control plane namespace")
	assert.Assert(t, !check(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}), "unmapped host namespace")
}
