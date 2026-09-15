package namespaces

import (
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewMapper maps virtual namespaces to host namespaces through sync.toHost.namespaces.mappings. Unmapped virtual
// namespaces resolve to the control plane namespace, where their objects live with rewritten names; the namespace
// syncer skips those. The mapper is deterministic, so it is deliberately not wrapped in the store recorder.
func NewMapper(ctx *synccontext.RegisterContext) synccontext.Mapper {
	if ctx.Config.Sync.ToHost.Namespaces.MappingsOnly {
		klog.Warning("sync.toHost.namespaces.mappingsOnly is not enforced by this build; unmapped virtual namespaces sync into the control plane namespace")
	}

	return &mapper{
		hostNamespace: ctx.Config.HostNamespace,
		mappings:      ctx.Config.Sync.ToHost.Namespaces.Mappings.ByName,
	}
}

type mapper struct {
	hostNamespace string
	mappings      map[string]string
}

func (m *mapper) Migrate(_ *synccontext.RegisterContext, _ synccontext.Mapper) error {
	return nil
}

func (m *mapper) GroupVersionKind() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("Namespace")
}

func (m *mapper) VirtualToHost(_ *synccontext.SyncContext, req types.NamespacedName, _ client.Object) types.NamespacedName {
	if req.Name == "" {
		return types.NamespacedName{}
	}

	pNamespace, ok := TranslateVirtualNamespace(translate.VClusterName, req.Name, m.mappings)
	if !ok {
		return types.NamespacedName{Name: m.hostNamespace}
	}

	return types.NamespacedName{Name: pNamespace}
}

func (m *mapper) HostToVirtual(_ *synccontext.SyncContext, req types.NamespacedName, _ client.Object) types.NamespacedName {
	if req.Name == m.hostNamespace {
		return types.NamespacedName{}
	}

	vNamespace, ok := TranslateHostNamespace(translate.VClusterName, req.Name, m.mappings)
	if !ok {
		return types.NamespacedName{}
	}

	return types.NamespacedName{Name: vNamespace}
}

func (m *mapper) IsManaged(ctx *synccontext.SyncContext, pObj client.Object) (bool, error) {
	if m.HostToVirtual(ctx, types.NamespacedName{Name: pObj.GetName()}, pObj).Name == "" {
		return false, nil
	}

	// a host namespace created or imported by another vCluster is never ours
	if marker := pObj.GetLabels()[translate.MarkerLabel]; marker != "" && marker != translate.Default.MarkerLabelCluster() {
		return false, nil
	}

	return true, nil
}
