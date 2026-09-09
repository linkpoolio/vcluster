package resources

import (
	"github.com/loft-sh/vcluster/pkg/mappings/generic"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
)

func CreateNodesMapper(_ *synccontext.RegisterContext) (synccontext.Mapper, error) {
	return generic.NewAnnotationNodeMapper(), nil
}
