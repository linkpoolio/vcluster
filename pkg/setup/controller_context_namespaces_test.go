package setup

import (
	"maps"
	"slices"
	"testing"

	rootconfig "github.com/loft-sh/vcluster/config"
	pkgconfig "github.com/loft-sh/vcluster/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

func namespaceSyncOptions() *pkgconfig.VirtualClusterConfig {
	options := &pkgconfig.VirtualClusterConfig{}
	options.Name = "tenant"
	options.HostNamespace = "tenant-control-plane"
	options.Sync.ToHost.Namespaces.Enabled = true
	options.Sync.ToHost.Namespaces.MappingsOnly = true
	options.Sync.ToHost.Namespaces.Mappings.ByName = map[string]string{
		"default":     "${name}-ns-default",
		"kube-system": "${name}-ns-kube-system",
	}
	return options
}

func cacheNamespaces(cacheOptions cache.Options) []string {
	return slices.Sorted(maps.Keys(cacheOptions.DefaultNamespaces))
}

func TestLocalCacheWatchesHostNamespaceWithoutNamespaceSync(t *testing.T) {
	options := &pkgconfig.VirtualClusterConfig{}
	options.HostNamespace = "tenant-control-plane"

	got := cacheNamespaces(getLocalCacheOptions(options))
	if !slices.Equal(got, []string{"tenant-control-plane"}) {
		t.Fatalf("expected host namespace only, got %v", got)
	}
}

func TestLocalCacheWatchesMappedNamespacesWithMappingsOnly(t *testing.T) {
	got := cacheNamespaces(getLocalCacheOptions(namespaceSyncOptions()))
	want := []string{"tenant-control-plane", "tenant-ns-default", "tenant-ns-kube-system"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestLocalCacheMergesAdditionalSecretNamespacesWithMappedNamespaces(t *testing.T) {
	options := namespaceSyncOptions()
	options.ExportKubeConfig.AdditionalSecrets = []rootconfig.ExportKubeConfigAdditionalSecretReference{
		{Namespace: "kubeconfigs", Name: "tenant-kubeconfig"},
	}

	got := cacheNamespaces(getLocalCacheOptions(options))
	want := []string{"kubeconfigs", "tenant-control-plane", "tenant-ns-default", "tenant-ns-kube-system"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestLocalCacheWatchesAllNamespacesWhenNamespaceSyncIsNotRestricted(t *testing.T) {
	tests := map[string]func(*pkgconfig.VirtualClusterConfig){
		"mappings only disabled": func(options *pkgconfig.VirtualClusterConfig) {
			options.Sync.ToHost.Namespaces.MappingsOnly = false
		},
		"no mappings": func(options *pkgconfig.VirtualClusterConfig) {
			options.Sync.ToHost.Namespaces.Mappings.ByName = nil
		},
		"pattern host mapping": func(options *pkgconfig.VirtualClusterConfig) {
			options.Sync.ToHost.Namespaces.Mappings.ByName["team-*"] = "${name}-team-*"
		},
		"additional secrets without restriction": func(options *pkgconfig.VirtualClusterConfig) {
			options.Sync.ToHost.Namespaces.MappingsOnly = false
			options.ExportKubeConfig.AdditionalSecrets = []rootconfig.ExportKubeConfigAdditionalSecretReference{
				{Namespace: "kubeconfigs", Name: "tenant-kubeconfig"},
			}
		},
	}

	for name, modify := range tests {
		t.Run(name, func(t *testing.T) {
			options := namespaceSyncOptions()
			modify(options)

			cacheOptions := getLocalCacheOptions(options)
			if cacheOptions.DefaultNamespaces != nil {
				t.Fatalf("expected cache for all namespaces, got %v", cacheNamespaces(cacheOptions))
			}
		})
	}
}

func TestLocalCacheStaysScopedWithFeaturesUsingOwnCaches(t *testing.T) {
	// the virtual scheduler, hybrid scheduling and csi storage capacities
	// watch the host through their own caches
	options := namespaceSyncOptions()
	options.ControlPlane.Advanced.VirtualScheduler.Enabled = true
	options.Sync.ToHost.Pods.HybridScheduling.Enabled = true
	options.Sync.FromHost.CSIStorageCapacities.Enabled = "true"

	got := cacheNamespaces(getLocalCacheOptions(options))
	want := []string{"tenant-control-plane", "tenant-ns-default", "tenant-ns-kube-system"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestLocalCacheWatchesGatewaySourceNamespacesWithMappedNamespaces(t *testing.T) {
	options := namespaceSyncOptions()
	options.Sync.FromHost.Gateways.Enabled = true
	options.Sync.FromHost.Gateways.Mappings.ByName = map[string]string{
		"platform-gateways/*": "shared-gateways/*",
	}

	cacheOptions := getLocalCacheOptions(options)
	got := cacheNamespaces(cacheOptions)
	want := []string{"tenant-control-plane", "tenant-ns-default", "tenant-ns-kube-system"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected default local cache %v, got %v", want, got)
	}

	gatewayNamespaces := slices.Sorted(maps.Keys(gatewayCacheNamespaces(t, cacheOptions)))
	want = []string{"platform-gateways", "tenant-control-plane", "tenant-ns-default", "tenant-ns-kube-system"}
	if !slices.Equal(gatewayNamespaces, want) {
		t.Fatalf("expected Gateway cache %v, got %v", want, gatewayNamespaces)
	}
}
