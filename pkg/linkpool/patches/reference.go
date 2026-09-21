package patches

import (
	"github.com/loft-sh/vcluster/config"
	"github.com/loft-sh/vcluster/pkg/mappings"
	"github.com/loft-sh/vcluster/pkg/mappings/generic"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// applyReference rewrites the referenced object name (and namespace, when a namespacePath is given) at m.
// toHost translates virtual -> host through the registered mapper for the kind, falling back to the translator's
// short host name (raw name in a synced namespace, "vxxxxxxxxx" in single-namespace mode). The reverse direction
// uses the mapper or the mapping store and leaves the value alone when neither knows the object.
func applyReference(ctx *synccontext.SyncContext, m match, ref *config.TranslatePatchReference, objNamespace string, toHost bool) {
	nameMatch, ok := relative(m, ref.NamePath)
	if !ok {
		return
	}
	name, ok := nameMatch.value.(string)
	if !ok || name == "" {
		return
	}

	namespace := objNamespace
	var nsMatch match
	hasNsPath := false
	if ref.NamespacePath != "" {
		if candidate, ok := relative(m, ref.NamespacePath); ok {
			if ns, ok := candidate.value.(string); ok && ns != "" {
				nsMatch, hasNsPath, namespace = candidate, true, ns
			}
		}
	}

	gvk := referenceGVK(m, ref)
	var translated types.NamespacedName
	if toHost {
		if ctx.Mappings != nil && ctx.Mappings.Has(gvk) {
			translated = mappings.VirtualToHost(ctx, name, namespace, gvk)
		} else {
			translated = translate.Default.HostNameShort(ctx, name, namespace)
			if err := generic.RecordMapping(ctx, translated, types.NamespacedName{Name: name, Namespace: namespace}, gvk); err != nil {
				return
			}
		}
	} else {
		if ctx.Mappings != nil && ctx.Mappings.Has(gvk) {
			translated = mappings.HostToVirtual(ctx, name, namespace, nil, gvk)
		} else if vName, ok := generic.HostToVirtualFromStore(ctx, types.NamespacedName{Name: name, Namespace: namespace}, gvk); ok {
			translated = vName
		}
	}
	if translated.Name == "" {
		return
	}

	nameMatch.set(translated.Name)
	if hasNsPath && translated.Namespace != "" {
		nsMatch.set(translated.Namespace)
	}
}

func referenceGVK(m match, ref *config.TranslatePatchReference) schema.GroupVersionKind {
	apiVersion, kind := ref.APIVersion, ref.Kind
	if v, ok := relative(m, ref.APIVersionPath); ok && ref.APIVersionPath != "" {
		if s, ok := v.value.(string); ok && s != "" {
			apiVersion = s
		}
	}
	if v, ok := relative(m, ref.KindPath); ok && ref.KindPath != "" {
		if s, ok := v.value.(string); ok && s != "" {
			kind = s
		}
	}
	return schema.FromAPIVersionAndKind(apiVersion, kind)
}
