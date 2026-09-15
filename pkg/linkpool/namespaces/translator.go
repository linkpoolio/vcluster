package namespaces

import (
	"sync/atomic"

	"github.com/loft-sh/vcluster/pkg/scheme"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var _ translate.Translator = &SyncedNamespaces{}

// NewTranslator returns a translator for sync.toHost.namespaces. Virtual namespaces matched by mappings are synced
// to their own host namespace and keep their object names. Unmapped virtual namespaces fall back to
// single-namespace behaviour, i.e. rewritten names inside hostNamespace, unless mappingsOnly is set, in which case
// their objects get no host name and are not synced.
func NewTranslator(hostNamespace string, mappings map[string]string) *SyncedNamespaces {
	return &SyncedNamespaces{
		hostNamespace: hostNamespace,
		mappings:      mappings,
		single:        translate.NewSingleNamespaceTranslator(hostNamespace),
	}
}

type SyncedNamespaces struct {
	hostNamespace string
	mappings      map[string]string
	mappingsOnly  atomic.Bool

	single translate.Translator
}

// SetMappingsOnly switches unmapped virtual namespaces from the control plane namespace fallback to not being
// synced at all. The pro hook that builds the translator does not receive this flag, so the mapper sets it from
// the full config before any controller starts.
func (s *SyncedNamespaces) SetMappingsOnly(mappingsOnly bool) {
	s.mappingsOnly.Store(mappingsOnly)
}

func (s *SyncedNamespaces) SingleNamespaceTarget() bool {
	return false
}

func (s *SyncedNamespaces) mappedHostNamespace(vNamespace string) (string, bool) {
	return TranslateVirtualNamespace(translate.VClusterName, vNamespace, s.mappings)
}

func (s *SyncedNamespaces) mappedVirtualNamespace(pNamespace string) (string, bool) {
	if pNamespace == s.hostNamespace {
		return "", false
	}
	return TranslateHostNamespace(translate.VClusterName, pNamespace, s.mappings)
}

func (s *SyncedNamespaces) HostName(ctx *synccontext.SyncContext, vName, vNamespace string) types.NamespacedName {
	if vName == "" {
		return types.NamespacedName{}
	}
	if pNamespace, ok := s.mappedHostNamespace(vNamespace); ok {
		return types.NamespacedName{Name: vName, Namespace: pNamespace}
	}
	if s.mappingsOnly.Load() {
		return types.NamespacedName{}
	}
	return s.single.HostName(ctx, vName, vNamespace)
}

func (s *SyncedNamespaces) HostNameShort(ctx *synccontext.SyncContext, vName, vNamespace string) types.NamespacedName {
	if vName == "" {
		return types.NamespacedName{}
	}
	if pNamespace, ok := s.mappedHostNamespace(vNamespace); ok {
		return types.NamespacedName{Name: vName, Namespace: pNamespace}
	}
	if s.mappingsOnly.Load() {
		return types.NamespacedName{}
	}
	return s.single.HostNameShort(ctx, vName, vNamespace)
}

func (s *SyncedNamespaces) HostNameCluster(vName string) string {
	return s.single.HostNameCluster(vName)
}

func (s *SyncedNamespaces) MarkerLabelCluster() string {
	return s.single.MarkerLabelCluster()
}

func (s *SyncedNamespaces) HostNamespace(ctx *synccontext.SyncContext, vNamespace string) string {
	if vNamespace == "" {
		return ""
	}
	if pNamespace, ok := s.mappedHostNamespace(vNamespace); ok {
		return pNamespace
	}
	if s.mappingsOnly.Load() {
		return ""
	}
	return s.single.HostNamespace(ctx, vNamespace)
}

func (s *SyncedNamespaces) IsTargetedNamespace(_ *synccontext.SyncContext, pNamespace string) bool {
	if pNamespace == s.hostNamespace {
		return true
	}
	_, ok := s.mappedVirtualNamespace(pNamespace)
	return ok
}

func (s *SyncedNamespaces) IsManaged(ctx *synccontext.SyncContext, pObj client.Object) bool {
	// cluster scoped objects and objects in the control plane namespace follow single-namespace rules
	if pObj.GetNamespace() == "" || pObj.GetNamespace() == s.hostNamespace {
		return s.single.IsManaged(ctx, pObj)
	}

	if !s.IsTargetedNamespace(ctx, pObj.GetNamespace()) {
		return false
	}

	// objects in mapped namespaces keep their names, so only the annotations tell us if we created them
	annotations := pObj.GetAnnotations()
	if annotations[translate.NameAnnotation] == "" {
		return false
	}
	if marker := pObj.GetLabels()[translate.MarkerLabel]; marker != "" && marker != translate.VClusterName {
		return false
	}
	if annotations[translate.KindAnnotation] != "" {
		gvk, err := apiutil.GVKForObject(pObj, scheme.Scheme)
		if err == nil && gvk.String() != annotations[translate.KindAnnotation] {
			return false
		}
	}
	if annotations[translate.HostNameAnnotation] != "" && annotations[translate.HostNameAnnotation] != pObj.GetName() {
		return false
	}
	if annotations[translate.HostNamespaceAnnotation] != "" && annotations[translate.HostNamespaceAnnotation] != pObj.GetNamespace() {
		return false
	}

	return true
}

func (s *SyncedNamespaces) LabelsToTranslate() map[string]bool {
	return s.single.LabelsToTranslate()
}
