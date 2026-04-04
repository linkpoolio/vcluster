package pods

import (
	"context"
	"testing"

	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHostNodeHash(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		assert.Equal(t, hostNodeHash("host-node-1"), hostNodeHash("host-node-1"))
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		assert.Assert(t, hostNodeHash("host-node-1") != hostNodeHash("host-node-2"))
	})

	t.Run("12 characters", func(t *testing.T) {
		assert.Equal(t, len(hostNodeHash("host-node-1")), 12)
	})
}

func TestResolveVirtualNodeName(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)

	renamedNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-1",
			Labels: map[string]string{
				"vcluster.loft.sh/host-node-hash": hostNodeHash("host-node-1"),
			},
		},
	}

	vClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(renamedNode).
		Build()

	syncCtx := &synccontext.SyncContext{
		Context:       context.Background(),
		VirtualClient: vClient,
	}

	s := &podSyncer{}

	t.Run("resolves renamed node by hash", func(t *testing.T) {
		name := s.resolveVirtualNodeName(syncCtx, "host-node-1")
		assert.Equal(t, name, "worker-1")
	})

	t.Run("falls back to host name when no match", func(t *testing.T) {
		name := s.resolveVirtualNodeName(syncCtx, "unknown-node")
		assert.Equal(t, name, "unknown-node")
	})

	t.Run("falls back when no renamed nodes exist", func(t *testing.T) {
		emptyClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		emptySyncCtx := &synccontext.SyncContext{
			Context:       context.Background(),
			VirtualClient: emptyClient,
		}
		name := s.resolveVirtualNodeName(emptySyncCtx, "some-node")
		assert.Equal(t, name, "some-node")
	})
}
