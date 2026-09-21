// Package namespaces provides the sync.toHost.namespaces implementation that upstream ships only in vCluster Pro.
// It plugs into the pro hook variables from init() so the core syncer code stays untouched.
package namespaces

import (
	"strings"

	"github.com/loft-sh/vcluster/pkg/util/namespaces"
)

// TranslateVirtualNamespace returns host namespace name based on virtual namespace and mappings
func TranslateVirtualNamespace(vClusterName, virtualNamespace string, mappings map[string]string) (string, bool) {
	// Priority 1: Exact virtual name to exact host name match
	for vName, hName := range mappings {
		if !namespaces.IsPattern(hName) && !namespaces.IsPattern(vName) {
			if namespaces.ProcessNamespaceName(vName, vClusterName) == virtualNamespace {
				return namespaces.ProcessNamespaceName(hName, vClusterName), true
			}
		}
	}

	// Priority 2: Pattern virtual name to pattern host name match
	for vPattern, hPattern := range mappings {
		if namespaces.IsPattern(hPattern) && namespaces.IsPattern(vPattern) {
			wildcardValue, matched := namespaces.MatchAndExtractWildcard(virtualNamespace, namespaces.ProcessNamespaceName(vPattern, vClusterName))
			if matched {
				return strings.Replace(namespaces.ProcessNamespaceName(hPattern, vClusterName), namespaces.WildcardChar, wildcardValue, 1), true
			}
		}
	}

	return "", false
}

// TranslateHostNamespace returns virtual namespace name based on host namespace and mappings
func TranslateHostNamespace(vClusterName, hostNamespace string, mappings map[string]string) (string, bool) {
	return namespaces.TranslateHostNamespace(vClusterName, hostNamespace, mappings)
}
