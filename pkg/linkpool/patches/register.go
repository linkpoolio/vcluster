package patches

import (
	"github.com/loft-sh/vcluster/config"
	"github.com/loft-sh/vcluster/pkg/pro"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	pro.ApplyPatchesHostObject = func(ctx *synccontext.SyncContext, _, pObj, vObj client.Object, patches []config.TranslatePatch, reverseExpressions bool) error {
		return ApplyHost(ctx, pObj, vObj, patches, reverseExpressions)
	}
	pro.ApplyPatchesVirtualObject = func(ctx *synccontext.SyncContext, _, vObj, pObj client.Object, patches []config.TranslatePatch, reverseExpressions bool) error {
		return ApplyVirtual(ctx, vObj, pObj, patches, reverseExpressions)
	}
}
