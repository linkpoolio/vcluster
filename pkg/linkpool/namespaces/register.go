package namespaces

import (
	"github.com/loft-sh/vcluster/config"
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
}
