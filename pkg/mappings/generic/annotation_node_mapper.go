package generic

import (
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const HostNodeNameAnnotation = "vcluster.loft.sh/host-node-name"

// NewAnnotationNodeMapper creates a node mapper that resolves host-to-virtual
// name mappings by reading the HostNodeNameAnnotation annotation on virtual nodes.
// This enables plugins to control node naming by setting the annotation during
// MutateCreateVirtual hooks. Falls back to passthrough (mirror) behavior when
// no annotation is found.
func NewAnnotationNodeMapper() *annotationNodeMapper {
	return &annotationNodeMapper{
		gvk: corev1.SchemeGroupVersion.WithKind("Node"),
	}
}

type annotationNodeMapper struct {
	gvk schema.GroupVersionKind
}

func (m *annotationNodeMapper) GroupVersionKind() schema.GroupVersionKind {
	return m.gvk
}

func (m *annotationNodeMapper) Migrate(_ *synccontext.RegisterContext, _ synccontext.Mapper) error {
	return nil
}

// HostToVirtual finds the virtual node that was created from the given host node
// by scanning for the HostNodeNameAnnotation annotation. Uses the cached virtual
// client so this is an in-memory lookup. Returns the host name unchanged if no
// annotated node is found (mirror fallback).
func (m *annotationNodeMapper) HostToVirtual(ctx *synccontext.SyncContext, req types.NamespacedName, _ client.Object) types.NamespacedName {
	nodeList := &corev1.NodeList{}
	if err := ctx.VirtualClient.List(ctx, nodeList); err != nil {
		klog.V(1).Infof("annotation node mapper: failed to list virtual nodes: %v", err)
		return req
	}

	for i := range nodeList.Items {
		ann := nodeList.Items[i].GetAnnotations()
		if ann[HostNodeNameAnnotation] == req.Name {
			return types.NamespacedName{Name: nodeList.Items[i].Name}
		}
	}

	return types.NamespacedName{Name: req.Name}
}

// VirtualToHost reads the HostNodeNameAnnotation from the virtual node to find
// the original host name. Returns the virtual name unchanged if the node has no
// annotation (mirror fallback).
func (m *annotationNodeMapper) VirtualToHost(ctx *synccontext.SyncContext, req types.NamespacedName, _ client.Object) types.NamespacedName {
	node := &corev1.Node{}
	if err := ctx.VirtualClient.Get(ctx, req, node); err != nil {
		return req
	}

	if hostName := node.GetAnnotations()[HostNodeNameAnnotation]; hostName != "" {
		return types.NamespacedName{Name: hostName}
	}

	return req
}

func (m *annotationNodeMapper) IsManaged(_ *synccontext.SyncContext, _ client.Object) (bool, error) {
	return true, nil
}
