package namespaces

import (
	"github.com/loft-sh/vcluster/pkg/scheme"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var _ translate.Translator = &syncedNamespaces{}

// NewTranslator returns a translator for sync.toHost.namespaces. Virtual namespaces matched by mappings are synced
// to their own host namespace and keep their object names. Unmapped virtual namespaces fall back to
// single-namespace behaviour, i.e. rewritten names inside hostNamespace.
func NewTranslator(hostNamespace string, mappings map[string]string) translate.Translator {
	return &syncedNamespaces{
		hostNamespace: hostNamespace,
		mappings:      mappings,
		single:        translate.NewSingleNamespaceTranslator(hostNamespace),
	}
}

type syncedNamespaces struct {
	hostNamespace string
	mappings      map[string]string

	single translate.Translator
}

func (s *syncedNamespaces) SingleNamespaceTarget() bool {
	return false
}

func (s *syncedNamespaces) mappedHostNamespace(vNamespace string) (string, bool) {
	return TranslateVirtualNamespace(translate.VClusterName, vNamespace, s.mappings)
}

func (s *syncedNamespaces) mappedVirtualNamespace(pNamespace string) (string, bool) {
	if pNamespace == s.hostNamespace {
		return "", false
	}
	return TranslateHostNamespace(translate.VClusterName, pNamespace, s.mappings)
}

func (s *syncedNamespaces) HostName(ctx *synccontext.SyncContext, vName, vNamespace string) types.NamespacedName {
	if vName == "" {
		return types.NamespacedName{}
	}
	if pNamespace, ok := s.mappedHostNamespace(vNamespace); ok {
		return types.NamespacedName{Name: vName, Namespace: pNamespace}
	}
	return s.single.HostName(ctx, vName, vNamespace)
}

func (s *syncedNamespaces) HostNameShort(ctx *synccontext.SyncContext, vName, vNamespace string) types.NamespacedName {
	if vName == "" {
		return types.NamespacedName{}
	}
	if pNamespace, ok := s.mappedHostNamespace(vNamespace); ok {
		return types.NamespacedName{Name: vName, Namespace: pNamespace}
	}
	return s.single.HostNameShort(ctx, vName, vNamespace)
}

func (s *syncedNamespaces) HostNameCluster(vName string) string {
	return s.single.HostNameCluster(vName)
}

func (s *syncedNamespaces) MarkerLabelCluster() string {
	return s.single.MarkerLabelCluster()
}

func (s *syncedNamespaces) HostNamespace(ctx *synccontext.SyncContext, vNamespace string) string {
	if vNamespace == "" {
		return ""
	}
	if pNamespace, ok := s.mappedHostNamespace(vNamespace); ok {
		return pNamespace
	}
	return s.single.HostNamespace(ctx, vNamespace)
}

func (s *syncedNamespaces) IsTargetedNamespace(_ *synccontext.SyncContext, pNamespace string) bool {
	if pNamespace == s.hostNamespace {
		return true
	}
	_, ok := s.mappedVirtualNamespace(pNamespace)
	return ok
}

func (s *syncedNamespaces) IsManaged(ctx *synccontext.SyncContext, pObj client.Object) bool {
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

func (s *syncedNamespaces) LabelsToTranslate() map[string]bool {
	return s.single.LabelsToTranslate()
}
