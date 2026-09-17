package namespaces

import (
	"context"

	"github.com/loft-sh/vcluster/config"
	vclusterconfig "github.com/loft-sh/vcluster/pkg/config"
	"github.com/loft-sh/vcluster/pkg/pro"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"github.com/loft-sh/vcluster/pkg/util/translate"
)

// current is the translator installed as translate.Default, so NewMapper can hand it the mappingsOnly flag that
// the translator hook does not receive.
var current *SyncedNamespaces

func init() {
	pro.GetNamespaceMapper = func(ctx *synccontext.RegisterContext, _ synccontext.Mapper) (synccontext.Mapper, error) {
		return NewMapper(ctx), nil
	}
	pro.GetWithSyncedNamespacesTranslator = func(hostNamespace string, mappings config.FromHostMappings) (translate.Translator, error) {
		current = NewTranslator(hostNamespace, mappings.ByName)
		return current, nil
	}

	// LicenseInit runs right after the translator is built and before the controller context loads the mapping
	// store, which verifies every stored mapping against the translator. mappingsOnly has to be known by then,
	// otherwise mappings into the control plane namespace recorded before namespace sync was enabled survive.
	licenseInit := pro.LicenseInit
	pro.LicenseInit = func(ctx context.Context, vConfig *vclusterconfig.VirtualClusterConfig) error {
		if current != nil {
			current.SetMappingsOnly(vConfig.Sync.ToHost.Namespaces.MappingsOnly)
		}
		return licenseInit(ctx, vConfig)
	}
}
