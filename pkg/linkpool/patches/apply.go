package patches

import (
	"fmt"

	"github.com/loft-sh/vcluster/config"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyHost patches the host object being created or updated from vObj. Expressions run forward unless
// reverseExpressions is set; references and labels translate virtual -> host.
func ApplyHost(ctx *synccontext.SyncContext, pObj, vObj client.Object, patches []config.TranslatePatch, reverseExpressions bool) error {
	return apply(ctx, pObj, vObj, patches, true, reverseExpressions)
}

// ApplyVirtual patches the virtual object being created or updated from pObj. Expressions run in reverse unless
// reverseExpressions is set (fromHost resources, where the virtual object is the derived one); references and
// labels translate host -> virtual.
func ApplyVirtual(ctx *synccontext.SyncContext, vObj, pObj client.Object, patches []config.TranslatePatch, reverseExpressions bool) error {
	return apply(ctx, vObj, pObj, patches, false, reverseExpressions)
}

func apply(ctx *synccontext.SyncContext, target, other client.Object, patches []config.TranslatePatch, toHost, reverseExpressions bool) error {
	if len(patches) == 0 || target == nil {
		return nil
	}

	targetMap, err := toMap(target)
	if err != nil {
		return err
	}
	otherMap := map[string]any{}
	if other != nil {
		if otherMap, err = toMap(other); err != nil {
			return err
		}
	}

	var vObjMap, hostObjMap map[string]any
	var vNamespace string
	if toHost {
		vObjMap, hostObjMap = otherMap, targetMap
		if other != nil {
			vNamespace = other.GetNamespace()
		}
	} else {
		vObjMap, hostObjMap = targetMap, otherMap
		vNamespace = target.GetNamespace()
	}

	for _, patch := range patches {
		segments, err := parsePath(patch.Path)
		if err != nil {
			return err
		}
		matches := find(targetMap, segments)
		if len(matches) == 0 {
			continue
		}

		for _, m := range matches {
			switch {
			case patch.Reference != nil:
				if m.value == nil {
					continue
				}
				applyReference(ctx, m, patch.Reference, objectNamespace(targetMap, otherMap), toHost)
			case patch.Labels != nil:
				if m.value == nil {
					continue
				}
				applyLabels(m, vNamespace, toHost)
			default:
				expression := patch.Expression
				if toHost == reverseExpressions {
					expression = patch.ReverseExpression
				}
				if expression == "" {
					continue
				}
				result, remove, err := evalExpression(expression, m.value, map[string]any{
					"vObject":    vObjMap,
					"hostObject": hostObjMap,
					"path":       patch.Path,
				})
				if err != nil {
					return fmt.Errorf("patch %q: %w", patch.Path, err)
				}
				if remove {
					m.remove()
				} else {
					m.set(result)
				}
			}
		}
	}

	return fromMap(targetMap, target)
}

// objectNamespace is the namespace references resolve in when no namespacePath is given: the namespace of the
// object we translate from (the virtual object when producing the host object and vice versa), falling back to
// the target's own namespace.
func objectNamespace(targetMap, sourceMap map[string]any) string {
	for _, m := range []map[string]any{sourceMap, targetMap} {
		if ns, ok := lookup(m, []string{"metadata", "namespace"}); ok {
			if s, ok := ns.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func toMap(obj client.Object) (map[string]any, error) {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u.Object, nil
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("convert %T to unstructured: %w", obj, err)
	}
	return m, nil
}

func fromMap(m map[string]any, obj client.Object) error {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		u.Object = m
		return nil
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(m, obj); err != nil {
		return fmt.Errorf("convert unstructured back to %T: %w", obj, err)
	}
	return nil
}
