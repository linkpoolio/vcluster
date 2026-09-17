package namespaces

import (
	"context"
	"testing"

	"github.com/loft-sh/vcluster/pkg/mappings"
	"github.com/loft-sh/vcluster/pkg/mappings/resources"
	"github.com/loft-sh/vcluster/pkg/mappings/store"
	"github.com/loft-sh/vcluster/pkg/pro"
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
func newMapperForTest(t *testing.T, mappingsOnly bool) (*synccontext.SyncContext, synccontext.Mapper) {
	storeBackend := store.NewMemoryBackend()
	mappingsStore, err := store.NewStore(context.TODO(), nil, nil, storeBackend)
	assert.NilError(t, err)

	vConfig := testingutil.NewFakeConfig()
	vConfig.Name = "tenant"
	vConfig.HostNamespace = "tenant-cp"
	vConfig.Sync.ToHost.Namespaces.Enabled = true
	vConfig.Sync.ToHost.Namespaces.MappingsOnly = mappingsOnly
	vConfig.Sync.ToHost.Namespaces.Mappings.ByName = map[string]string{
		"frontend": "customer-frontend",
		"team-*":   "${name}-team-*",
	}

	oldName, oldDefault := translate.VClusterName, translate.Default
	t.Cleanup(func() { translate.VClusterName, translate.Default = oldName, oldDefault })
	translate.VClusterName = "tenant"
	oldCurrent := current
	t.Cleanup(func() { current = oldCurrent })
	current = NewTranslator("tenant-cp", vConfig.Sync.ToHost.Namespaces.Mappings.ByName)
	translate.Default = current

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
	syncCtx, m := newMapperForTest(t, false)
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: "frontend"}, nil), types.NamespacedName{Name: "customer-frontend"})
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: "team-dev"}, nil), types.NamespacedName{Name: "tenant-team-dev"})
	// unmapped namespaces resolve to the control plane namespace so lookups like the DNS service work
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: "kube-system"}, nil), types.NamespacedName{Name: "tenant-cp"})
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: ""}, nil), types.NamespacedName{})
	assert.Equal(t, translate.Default.HostNamespace(syncCtx, "kube-system"), "tenant-cp")
}

func TestLicenseInitSetsMappingsOnly(t *testing.T) {
	oldName, oldDefault, oldCurrent := translate.VClusterName, translate.Default, current
	t.Cleanup(func() { translate.VClusterName, translate.Default, current = oldName, oldDefault, oldCurrent })
	translate.VClusterName = "tenant"

	vConfig := testingutil.NewFakeConfig()
	vConfig.HostNamespace = "tenant-cp"
	vConfig.Sync.ToHost.Namespaces.Enabled = true
	vConfig.Sync.ToHost.Namespaces.MappingsOnly = true
	vConfig.Sync.ToHost.Namespaces.Mappings.ByName = map[string]string{"team-*": "${name}-team-*"}

	tr, err := pro.GetWithSyncedNamespacesTranslator("tenant-cp", vConfig.Sync.ToHost.Namespaces.Mappings)
	assert.NilError(t, err)
	translate.Default = tr

	// before LicenseInit the control plane namespace is still a target (mappingsOnly unknown)
	assert.Assert(t, tr.IsTargetedNamespace(nil, "tenant-cp"))
	assert.NilError(t, pro.LicenseInit(context.TODO(), vConfig))
	assert.Assert(t, !tr.IsTargetedNamespace(nil, "tenant-cp"))
	assert.Equal(t, tr.HostNamespace(nil, "kube-system"), "")
}

func TestMapperMappingsOnly(t *testing.T) {
	syncCtx, m := newMapperForTest(t, true)
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: "team-dev"}, nil), types.NamespacedName{Name: "tenant-team-dev"})
	assert.DeepEqual(t, m.VirtualToHost(syncCtx, types.NamespacedName{Name: "kube-system"}, nil), types.NamespacedName{})

	// the mapper hands mappingsOnly to the translator built by the pro hook
	assert.Equal(t, translate.Default.HostNamespace(syncCtx, "team-dev"), "tenant-team-dev")
	assert.Equal(t, translate.Default.HostNamespace(syncCtx, "kube-system"), "")
	assert.DeepEqual(t, translate.Default.HostName(syncCtx, "nginx", "kube-system"), types.NamespacedName{})
	assert.DeepEqual(t, translate.Default.HostNameShort(syncCtx, "nginx", "kube-system"), types.NamespacedName{})

	// the control plane namespace is no longer a sync target, so stale single-namespace mappings and objects
	// there are ignored
	assert.Assert(t, !translate.Default.IsTargetedNamespace(syncCtx, "tenant-cp"))
	assert.Assert(t, translate.Default.IsTargetedNamespace(syncCtx, "tenant-team-dev"))
	stale := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "coredns-x-kube-system-x-tenant", Namespace: "tenant-cp",
		Labels:      map[string]string{translate.MarkerLabel: "tenant"},
		Annotations: map[string]string{translate.NameAnnotation: "coredns", translate.NamespaceAnnotation: "kube-system"},
	}}
	assert.Assert(t, !translate.Default.IsManaged(syncCtx, stale))
}

func TestMapperHostToVirtual(t *testing.T) {
	syncCtx, m := newMapperForTest(t, false)
	assert.DeepEqual(t, m.HostToVirtual(syncCtx, types.NamespacedName{Name: "customer-frontend"}, nil), types.NamespacedName{Name: "frontend"})
	assert.DeepEqual(t, m.HostToVirtual(syncCtx, types.NamespacedName{Name: "tenant-team-dev"}, nil), types.NamespacedName{Name: "team-dev"})
	assert.DeepEqual(t, m.HostToVirtual(syncCtx, types.NamespacedName{Name: "tenant-cp"}, nil), types.NamespacedName{})
	assert.DeepEqual(t, m.HostToVirtual(syncCtx, types.NamespacedName{Name: "kube-system"}, nil), types.NamespacedName{})
}

func TestMapperIsManaged(t *testing.T) {
	syncCtx, m := newMapperForTest(t, false)

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
