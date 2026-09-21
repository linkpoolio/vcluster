package patches

import (
	"github.com/loft-sh/vcluster/pkg/util/translate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// applyLabels treats the value at m as a label selector: either a plain map of labels (matchLabels style) or a
// full metav1.LabelSelector. Keys are translated like vCluster translates pod selectors, so a host selector still
// matches the synced pods and a virtual selector shows the tenant's original keys.
func applyLabels(m match, vNamespace string, toHost bool) {
	raw, ok := m.value.(map[string]any)
	if !ok || len(raw) == 0 {
		return
	}

	if isLabelSelector(raw) {
		selector := &metav1.LabelSelector{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, selector); err != nil {
			return
		}
		if toHost {
			selector = translate.HostLabelSelector(selector)
		} else {
			selector = translate.VirtualLabelSelector(selector)
		}
		out, err := runtime.DefaultUnstructuredConverter.ToUnstructured(selector)
		if err != nil {
			return
		}
		m.set(out)
		return
	}

	labels := map[string]string{}
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return
		}
		labels[k] = s
	}
	var translated map[string]string
	if toHost {
		translated = translate.HostLabelsMap(labels, nil, vNamespace, false)
	} else {
		translated = translate.VirtualLabelsMap(labels, nil)
	}
	out := make(map[string]any, len(translated))
	for k, v := range translated {
		out[k] = v
	}
	m.set(out)
}

func isLabelSelector(raw map[string]any) bool {
	_, hasMatchLabels := raw["matchLabels"]
	_, hasMatchExpressions := raw["matchExpressions"]
	return hasMatchLabels || hasMatchExpressions
}
