package persistentvolumes

import (
	"context"
	"testing"

	"github.com/loft-sh/vcluster/pkg/syncer/synccontext"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"k8s.io/apimachinery/pkg/runtime"
)

type testNodeMapper struct {
	mappings map[string]string
}

func (m *testNodeMapper) GroupVersionKind() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("Node")
}

func (m *testNodeMapper) Migrate(_ *synccontext.RegisterContext, _ synccontext.Mapper) error {
	return nil
}

func (m *testNodeMapper) HostToVirtual(_ *synccontext.SyncContext, req types.NamespacedName, _ client.Object) types.NamespacedName {
	if mapped, ok := m.mappings[req.Name]; ok {
		return types.NamespacedName{Name: mapped}
	}
	return req
}

func (m *testNodeMapper) VirtualToHost(_ *synccontext.SyncContext, req types.NamespacedName, _ client.Object) types.NamespacedName {
	return req
}

func (m *testNodeMapper) IsManaged(_ *synccontext.SyncContext, _ client.Object) (bool, error) {
	return true, nil
}

func TestTranslatePVNodeAffinity(t *testing.T) {
	mapper := &testNodeMapper{
		mappings: map[string]string{
			"host-node-1": "worker-1",
			"host-node-2": "worker-2",
		},
	}

	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	vClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	s := &persistentVolumeSyncer{
		nodesMapper:   mapper,
		virtualClient: vClient,
	}

	syncCtx := &synccontext.SyncContext{
		Context: context.Background(),
	}

	t.Run("rewrites node affinity values", func(t *testing.T) {
		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pv"},
			Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "some.io/nodename",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"host-node-1"},
									},
								},
							},
						},
					},
				},
			},
		}

		s.translatePVNodeAffinity(syncCtx, pv)

		vals := pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values
		assert.Equal(t, vals[0], "worker-1")
	})

	t.Run("leaves unmapped values unchanged", func(t *testing.T) {
		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pv-2"},
			Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "some.io/nodename",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"unknown-node"},
									},
								},
							},
						},
					},
				},
			},
		}

		s.translatePVNodeAffinity(syncCtx, pv)

		vals := pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values
		assert.Equal(t, vals[0], "unknown-node")
	})

	t.Run("no-op when node affinity is nil", func(t *testing.T) {
		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pv-3"},
		}
		s.translatePVNodeAffinity(syncCtx, pv)
	})
}
