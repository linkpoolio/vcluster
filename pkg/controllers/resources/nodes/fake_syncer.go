package nodes

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/loft-sh/vcluster/pkg/constants"
	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	syncer "github.com/loft-sh/vcluster/pkg/syncer/types"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/klog/v2"

	"github.com/loft-sh/vcluster/pkg/controllers/resources/nodes/nodeservice"
	podtranslate "github.com/loft-sh/vcluster/pkg/controllers/resources/pods/translate"
	"github.com/loft-sh/vcluster/pkg/util/random"
	"github.com/loft-sh/vcluster/pkg/util/translate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// FakeNodesVersion is the default version that will be used for fake nodes
	FakeNodesVersion = "v1.19.1"
)

func NewFakeSyncer(ctx *synccontext.RegisterContext, nodeService nodeservice.Provider) (syncer.Object, error) {
	return &fakeNodeSyncer{
		nodeServiceProvider:  nodeService,
		fakeKubeletIPs:       ctx.Config.Networking.Advanced.ProxyKubelets.ByIP,
		fakeKubeletHostnames: ctx.Config.Networking.Advanced.ProxyKubelets.ByHostname,
		translator:           newNodeNameTranslator(),
	}, nil
}

type fakeNodeSyncer struct {
	nodeServiceProvider  nodeservice.Provider
	fakeKubeletIPs       bool
	fakeKubeletHostnames bool
	translator           *nodeNameTranslator
}

func (r *fakeNodeSyncer) Resource() client.Object {
	return &corev1.Node{}
}

func (r *fakeNodeSyncer) Name() string {
	return "fake-node"
}

var _ syncer.IndicesRegisterer = &fakeNodeSyncer{}

func (r *fakeNodeSyncer) RegisterIndices(ctx *synccontext.RegisterContext) error {
	return registerIndices(ctx)
}

var _ syncer.ControllerModifier = &fakeNodeSyncer{}

func (r *fakeNodeSyncer) ModifyController(ctx *synccontext.RegisterContext, builder *builder.Builder) (*builder.Builder, error) {
	return modifyController(ctx, r.nodeServiceProvider, builder)
}

var _ syncer.FakeSyncer = &fakeNodeSyncer{}

func (r *fakeNodeSyncer) FakeSyncToVirtual(ctx *synccontext.SyncContext, name types.NamespacedName) (ctrl.Result, error) {
	realName := name.Name

	// The reconciler looks up nodes by the reconcile request name. When a host pod
	// triggers reconciliation for a real hostname, the virtual node won't be found
	// by that name because it has a translated name. Check if we already created a
	// node for this host by looking up the hash label.
	hash := HostNodeHash(realName)
	nodeList := &corev1.NodeList{}
	err := ctx.VirtualClient.List(ctx, nodeList, client.MatchingLabels{
		HostNodeHashLabel: hash,
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(nodeList.Items) > 0 {
		existing := &nodeList.Items[0]
		r.translator.RegisterExisting(realName, existing.Name)
		return r.FakeSync(ctx, existing)
	}

	needed, err := r.nodeNeeded(ctx, realName)
	if err != nil {
		return ctrl.Result{}, err
	} else if !needed {
		return ctrl.Result{}, nil
	}

	virtualName := r.translator.Translate(realName)
	klog.Infof("Create fake node %s (host: %s, hash: %s)", virtualName, realName, hash)
	return ctrl.Result{}, createFakeNode(ctx, r.fakeKubeletIPs, r.fakeKubeletHostnames, r.nodeServiceProvider, ctx.VirtualClient, virtualName, realName)
}

func (r *fakeNodeSyncer) FakeSync(ctx *synccontext.SyncContext, vObj client.Object) (ctrl.Result, error) {
	node, ok := vObj.(*corev1.Node)
	if !ok || node == nil {
		return ctrl.Result{}, fmt.Errorf("%#v is not a node", vObj)
	}

	// Recover name mapping from annotation on restart
	if node.Annotations != nil {
		if realName, ok := node.Annotations[HostNodeNameAnnotation]; ok {
			r.translator.RegisterExisting(realName, node.Name)
		}
	}

	needed, err := r.nodeNeeded(ctx, node.Name)
	if err != nil {
		return ctrl.Result{}, err
	} else if !needed {
		ctx.Log.Infof("Delete fake node %s as it is not needed anymore", vObj.GetName())
		return ctrl.Result{}, ctx.VirtualClient.Delete(ctx, vObj)
	}

	// check if we need to update node ips
	updated := r.updateIfNeeded(ctx, node, node.Name)
	if updated != nil {
		ctx.Log.Infof("Update fake node %s", node.Name)
		err := ctx.VirtualClient.Status().Update(ctx, updated)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("update node: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *fakeNodeSyncer) updateIfNeeded(ctx *synccontext.SyncContext, node *corev1.Node, name string) *corev1.Node {
	var updated *corev1.Node

	newAddresses := []corev1.NodeAddress{
		{
			Address: GetNodeHost(node.Name),
			Type:    corev1.NodeHostName,
		},
	}

	if r.fakeKubeletIPs {
		nodeIP, err := r.nodeServiceProvider.GetNodeIP(ctx, name)
		if err != nil {
			ctx.Log.Errorf("error getting fake node ip: %v", err)
		}

		newAddresses = append(newAddresses, corev1.NodeAddress{
			Address: nodeIP,
			Type:    corev1.NodeInternalIP,
		})
	}

	if !equality.Semantic.DeepEqual(node.Status.Addresses, newAddresses) {
		updated = node.DeepCopy()
		updated.Status.Addresses = newAddresses
	}

	return updated
}

func (r *fakeNodeSyncer) nodeNeeded(ctx *synccontext.SyncContext, nodeName string) (bool, error) {
	needed, err := isNodeNeededByPod(ctx, ctx.VirtualClient, ctx.HostClient, nodeName)
	if err != nil || needed {
		return needed, err
	}

	// Also check the complementary name: virtual pods use translated names,
	// physical pods use real hostnames.
	if virtualName, ok := r.translator.RealToVirtual(nodeName); ok {
		return isNodeNeededByPod(ctx, ctx.VirtualClient, ctx.HostClient, virtualName)
	}
	if realName, ok := r.translator.VirtualToReal(nodeName); ok {
		return isNodeNeededByPod(ctx, ctx.VirtualClient, ctx.HostClient, realName)
	}

	return false, nil
}

// this is not a real guid, but it doesn't really matter because it should just look right and not be an actual guid
func newGUID() string {
	return random.String(8) + "-" + random.String(4) + "-" + random.String(4) + "-" + random.String(4) + "-" + random.String(12)
}

func createFakeNode(
	ctx context.Context,
	fakeKubeletIPs bool,
	fakeKubeletHostnames bool,
	nodeServiceProvider nodeservice.Provider,
	virtualClient client.Client,
	virtualName string,
	realName string,
) error {
	nodeServiceProvider.Lock()
	defer nodeServiceProvider.Unlock()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: virtualName,
			Labels: map[string]string{
				"vcluster.loft.sh/fake-node": "true",
				HostNodeHashLabel:            HostNodeHash(realName),
				"beta.kubernetes.io/arch":    runtime.GOARCH,
				"beta.kubernetes.io/os":      "linux",
				"kubernetes.io/arch":         runtime.GOARCH,
				"kubernetes.io/hostname":     translate.SafeConcatName("fake", virtualName),
				"kubernetes.io/os":           "linux",
			},
			Annotations: map[string]string{
				"node.alpha.kubernetes.io/ttl":                           "0",
				"volumes.kubernetes.io/controller-managed-attach-detach": "false",
				HostNodeNameAnnotation:                                   realName,
			},
		},
	}

	err := virtualClient.Create(ctx, node)
	if err != nil {
		return err
	}

	orig := node.DeepCopy()
	node.Status = corev1.NodeStatus{
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:                     resource.MustParse("16"),
			corev1.ResourceMemory:                  resource.MustParse("32Gi"),
			corev1.ResourceEphemeralStorage:        resource.MustParse("100Gi"),
			corev1.ResourceHugePagesPrefix + "1Gi": resource.MustParse("0"),
			corev1.ResourceHugePagesPrefix + "2Mi": resource.MustParse("0"),
			corev1.ResourcePods:                    resource.MustParse("110"),
		},
		Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:                     resource.MustParse("16"),
			corev1.ResourceMemory:                  resource.MustParse("32Gi"),
			corev1.ResourceEphemeralStorage:        resource.MustParse("100Gi"),
			corev1.ResourceHugePagesPrefix + "1Gi": resource.MustParse("0"),
			corev1.ResourceHugePagesPrefix + "2Mi": resource.MustParse("0"),
			corev1.ResourcePods:                    resource.MustParse("110"),
		},
		Conditions: []corev1.NodeCondition{
			{
				LastHeartbeatTime:  metav1.Now(),
				LastTransitionTime: metav1.Now(),
				Message:            "kubelet has sufficient memory available",
				Reason:             "KubeletHasSufficientMemory",
				Status:             "False",
				Type:               corev1.NodeMemoryPressure,
			},
			{
				LastHeartbeatTime:  metav1.Now(),
				LastTransitionTime: metav1.Now(),
				Message:            "kubelet has no disk pressure",
				Reason:             "KubeletHasNoDiskPressure",
				Status:             "False",
				Type:               corev1.NodeDiskPressure,
			},
			{
				LastHeartbeatTime:  metav1.Now(),
				LastTransitionTime: metav1.Now(),
				Message:            "kubelet has sufficient PID available",
				Reason:             "KubeletHasSufficientPID",
				Status:             "False",
				Type:               corev1.NodePIDPressure,
			},
			{
				LastHeartbeatTime:  metav1.Now(),
				LastTransitionTime: metav1.Now(),
				Message:            "kubelet is posting ready status",
				Reason:             "KubeletReady",
				Status:             "True",
				Type:               corev1.NodeReady,
			},
		},
		Addresses: []corev1.NodeAddress{},
		DaemonEndpoints: corev1.NodeDaemonEndpoints{
			KubeletEndpoint: corev1.DaemonEndpoint{
				Port: constants.KubeletPort,
			},
		},
		NodeInfo: corev1.NodeSystemInfo{
			Architecture:            "amd64",
			BootID:                  newGUID(),
			ContainerRuntimeVersion: "docker://19.3.12",
			KernelVersion:           "4.19.76-fakelinux",
			KubeProxyVersion:        FakeNodesVersion, //nolint:staticcheck //deprecated, but we should continue to use it until the api removes it
			KubeletVersion:          FakeNodesVersion,
			MachineID:               newGUID(),
			SystemUUID:              newGUID(),
			OperatingSystem:         "linux",
			OSImage:                 "Fake Kubernetes Image",
		},
		Images: []corev1.ContainerImage{},
	}

	if fakeKubeletHostnames {
		node.Status.Addresses = append(node.Status.Addresses, corev1.NodeAddress{
			Address: GetNodeHost(node.Name),
			Type:    corev1.NodeHostName,
		})
	}

	if fakeKubeletIPs {
		nodeIP, err := nodeServiceProvider.GetNodeIP(ctx, virtualName)
		if err != nil {
			return fmt.Errorf("create fake node ip: %w", err)
		}

		node.Status.Addresses = append(node.Status.Addresses, corev1.NodeAddress{
			Address: nodeIP,
			Type:    corev1.NodeInternalIP,
		})
	}

	err = virtualClient.Status().Patch(ctx, node, client.MergeFrom(orig))
	if err != nil {
		return err
	}

	// remove not ready taints
	orig = node.DeepCopy()
	node.Spec.Taints = []corev1.Taint{}
	err = virtualClient.Patch(ctx, node, client.MergeFrom(orig))
	if err != nil {
		return err
	}

	return nil
}

// Filter away  virtual DaemonSet Pods using OwnerReferences to enable scale down
func filterOutVirtualDaemonSets(pl *corev1.PodList) []corev1.Pod {
	var podsNoDaemonSets []corev1.Pod

	for _, item := range pl.Items {
		var isDaemonSet bool

		// ensure pod has owner references
		if len(item.OwnerReferences) > 0 {
			// cover edge case with multiple owner refs
			for _, ownerRef := range item.OwnerReferences {
				if ownerRef.APIVersion == "apps/v1" && ownerRef.Kind == "DaemonSet" {
					isDaemonSet = true
				}
			}
		}
		if !isDaemonSet {
			podsNoDaemonSets = append(podsNoDaemonSets, item)
		}
	}

	return podsNoDaemonSets
}

// Filter away physical DaemonSet Pods using annotations to enable scale down
func filterOutPhysicalDaemonSets(pl *corev1.PodList) []corev1.Pod {
	var podsNoDaemonSets []corev1.Pod

	for _, item := range pl.Items {
		if item.Annotations == nil || item.Annotations[podtranslate.OwnerSetKind] != "DaemonSet" {
			podsNoDaemonSets = append(podsNoDaemonSets, item)
		}
	}
	return podsNoDaemonSets
}

func GetNodeHost(nodeName string) string {
	return strings.ReplaceAll(nodeName, ".", "-") + "." + constants.NodeSuffix
}

// GetNodeHostLegacy returns Node hostname in a format used in 0.14.x release.
// This function is added for backwards compatibility and may be removed in a future release.
func GetNodeHostLegacy(nodeName, currentNamespace string) string {
	return strings.ReplaceAll(nodeName, ".", "-") + "." + translate.VClusterName + "." + currentNamespace + "." + constants.NodeSuffix
}
