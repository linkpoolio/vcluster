package patches

import (
	"context"
	"testing"

	"github.com/loft-sh/vcluster/config"
	"github.com/loft-sh/vcluster/pkg/mappings"
	"github.com/loft-sh/vcluster/pkg/mappings/store"
	"github.com/loft-sh/vcluster/pkg/pro"
	"github.com/loft-sh/vcluster/pkg/scheme"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	testingutil "github.com/loft-sh/vcluster/pkg/util/testing"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newSyncContext(t *testing.T) *synccontext.SyncContext {
	storeBackend := store.NewMemoryBackend()
	mappingsStore, err := store.NewStore(context.TODO(), nil, nil, storeBackend)
	assert.NilError(t, err)

	oldName, oldDefault := translate.VClusterName, translate.Default
	t.Cleanup(func() { translate.VClusterName, translate.Default = oldName, oldDefault })
	translate.VClusterName = "tenant"
	translate.Default = translate.NewSingleNamespaceTranslator("tenant-cp")

	vConfig := testingutil.NewFakeConfig()
	vConfig.Name = "tenant"
	vConfig.HostNamespace = "tenant-cp"
	registerContext := &synccontext.RegisterContext{
		Context:        context.TODO(),
		Config:         vConfig,
		Mappings:       mappings.NewMappingsRegistry(mappingsStore),
		HostManager:    testingutil.NewFakeManager(testingutil.NewFakeClient(scheme.Scheme)),
		VirtualManager: testingutil.NewFakeManager(testingutil.NewFakeClient(scheme.Scheme)),
	}
	return registerContext.ToSyncContext("test")
}

func TestExpressionToHostAndBack(t *testing.T) {
	ctx := newSyncContext(t)
	patches := []config.TranslatePatch{{
		Path:              "data.host",
		Expression:        "value.startsWith('www.') ? value.slice(4) : value",
		ReverseExpression: `"www." + value`,
	}}
	vObj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "team-a"}, Data: map[string]string{"host": "www.example.com"}}
	pObj := vObj.DeepCopy()

	assert.NilError(t, pro.ApplyPatchesHostObject(ctx, nil, pObj, vObj, patches, false))
	assert.Equal(t, pObj.Data["host"], "example.com")

	back := pObj.DeepCopy()
	assert.NilError(t, pro.ApplyPatchesVirtualObject(ctx, nil, back, pObj, patches, false))
	assert.Equal(t, back.Data["host"], "www.example.com")

	// fromHost resources: the virtual object is the derived one, so the forward expression applies to it
	derived := vObj.DeepCopy()
	assert.NilError(t, pro.ApplyPatchesVirtualObject(ctx, nil, derived, vObj, patches, true))
	assert.Equal(t, derived.Data["host"], "example.com")
}

func TestExpressionAddsAndRemovesFields(t *testing.T) {
	ctx := newSyncContext(t)
	vNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}}
	pNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-team-a"}}

	add := []config.TranslatePatch{{
		Path:       "metadata.annotations",
		Expression: `Object.assign(value || {}, {"ipam.cilium.io/ip-pool": "pool-" + context.vObject.metadata.name})`,
	}}
	assert.NilError(t, ApplyHost(ctx, pNs, vNs, add, false))
	assert.Equal(t, pNs.Annotations["ipam.cilium.io/ip-pool"], "pool-team-a")

	remove := []config.TranslatePatch{{Path: "metadata.annotations", Expression: "undefined"}}
	assert.NilError(t, ApplyHost(ctx, pNs, vNs, remove, false))
	assert.Assert(t, pNs.Annotations == nil)

	// a path whose parent does not exist is skipped, not created
	skip := []config.TranslatePatch{{Path: "metadata.annotations.foo", Expression: `"x"`}}
	assert.NilError(t, ApplyHost(ctx, pNs, vNs, skip, false))
	assert.Assert(t, pNs.Annotations == nil)
}

func TestExpressionWildcardAndContext(t *testing.T) {
	ctx := newSyncContext(t)
	vObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.io/v1", "kind": "Thing",
		"metadata": map[string]any{"name": "t", "namespace": "team-a"},
		"spec":     map[string]any{"hosts": []any{"a", "prod-b"}},
	}}
	pObj := vObj.DeepCopy()
	patches := []config.TranslatePatch{{
		Path:       "spec.hosts[*]",
		Expression: "value.startsWith('prod-') ? value : `prod-${value}-${context.path === 'spec.hosts[*]' ? 'ok' : 'bad'}`",
	}}
	assert.NilError(t, ApplyHost(ctx, pObj, vObj, patches, false))
	assert.DeepEqual(t, pObj.Object["spec"].(map[string]any)["hosts"], []any{"prod-a-ok", "prod-b"})
}

func TestExpressionError(t *testing.T) {
	ctx := newSyncContext(t)
	vObj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "team-a"}, Data: map[string]string{"k": "v"}}
	err := ApplyHost(ctx, vObj.DeepCopy(), vObj, []config.TranslatePatch{{Path: "data.k", Expression: "value.nope()"}}, false)
	assert.ErrorContains(t, err, "evaluate expression")
}

func TestReferenceWithoutMapperUsesShortHostName(t *testing.T) {
	ctx := newSyncContext(t)
	vObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.io/v1", "kind": "Thing",
		"metadata": map[string]any{"name": "t", "namespace": "team-a"},
		"spec":     map[string]any{"secretName": "creds", "other": map[string]any{"name": "cfg", "namespace": "team-b"}},
	}}
	pObj := vObj.DeepCopy()
	patches := []config.TranslatePatch{
		{Path: "spec.secretName", Reference: &config.TranslatePatchReference{APIVersion: "v1", Kind: "Secret"}},
		{Path: "spec.other", Reference: &config.TranslatePatchReference{APIVersion: "v1", Kind: "ConfigMap", NamePath: "name", NamespacePath: "namespace"}},
	}
	assert.NilError(t, ApplyHost(ctx, pObj, vObj, patches, false))

	spec := pObj.Object["spec"].(map[string]any)
	expected := translate.Default.HostNameShort(ctx, "creds", "team-a")
	assert.Equal(t, spec["secretName"], expected.Name)
	assert.Assert(t, spec["secretName"] != "creds")

	other := spec["other"].(map[string]any)
	assert.Equal(t, other["name"], translate.Default.HostNameShort(ctx, "cfg", "team-b").Name)
	assert.Equal(t, other["namespace"], "tenant-cp")
}

func TestLabelsSelector(t *testing.T) {
	ctx := newSyncContext(t)
	vObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.io/v1", "kind": "Thing",
		"metadata": map[string]any{"name": "t", "namespace": "team-a"},
		"spec": map[string]any{
			"selector": map[string]any{"app": "demo"},
			"podSelector": map[string]any{"matchLabels": map[string]any{"app": "demo"}, "matchExpressions": []any{
				map[string]any{"key": "tier", "operator": "In", "values": []any{"web"}},
			}},
		},
	}}
	pObj := vObj.DeepCopy()
	patches := []config.TranslatePatch{
		{Path: "spec.selector", Labels: &config.TranslatePatchLabels{}},
		{Path: "spec.podSelector", Labels: &config.TranslatePatchLabels{}},
	}
	assert.NilError(t, ApplyHost(ctx, pObj, vObj, patches, false))

	spec := pObj.Object["spec"].(map[string]any)
	selector := spec["selector"].(map[string]any)
	assert.Equal(t, selector[translate.HostLabel("app")], "demo")
	assert.Equal(t, selector[translate.MarkerLabel], "tenant")
	assert.Equal(t, selector[translate.NamespaceLabel], "team-a")

	podSelector := spec["podSelector"].(map[string]any)
	matchLabels := podSelector["matchLabels"].(map[string]any)
	assert.Equal(t, matchLabels[translate.HostLabel("app")], "demo")
	expr := podSelector["matchExpressions"].([]any)[0].(map[string]any)
	assert.Equal(t, expr["key"], translate.HostLabel("tier"))

	// and back
	back := pObj.DeepCopy()
	assert.NilError(t, ApplyVirtual(ctx, back, pObj, patches[:1], false))
	assert.DeepEqual(t, back.Object["spec"].(map[string]any)["selector"], map[string]any{"app": "demo"})
}
