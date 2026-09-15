package namespaces

import (
	"github.com/loft-sh/vcluster/config"
	"github.com/loft-sh/vcluster/pkg/pro"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"github.com/loft-sh/vcluster/pkg/util/translate"
)

func init() {
	pro.GetNamespaceMapper = func(ctx *synccontext.RegisterContext, _ synccontext.Mapper) (synccontext.Mapper, error) {
		return NewMapper(ctx), nil
	}
	pro.GetWithSyncedNamespacesTranslator = func(hostNamespace string, mappings config.FromHostMappings) (translate.Translator, error) {
		return NewTranslator(hostNamespace, mappings.ByName), nil
	}
}
