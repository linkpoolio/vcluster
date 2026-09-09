package blockingcacheclient

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestBlockApplyNoopDoesNotTimeout(t *testing.T) {
	obj := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "vcluster-protected-apiservices",
			UID:             "uid-1",
			ResourceVersion: "1",
			Generation:      1,
		},
	}

	inner := fake.NewClientBuilder().WithObjects(obj.DeepCopy()).Build()
	c := &CacheClient{Client: inner, scheme: inner.Scheme()}

	pre := obj.DeepCopy()
	if err := c.blockApply(context.Background(), nil, obj, pre); err != nil {
		t.Fatalf("no-op apply with unchanged resourceVersion should succeed, got %v", err)
	}
}

func TestBlockApplyWaitsUntilCreated(t *testing.T) {
	obj := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "created"},
	}

	inner := fake.NewClientBuilder().Build()
	gets := 0
	wrapped := interceptor.NewClient(inner, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key types.NamespacedName, out client.Object, opts ...client.GetOption) error {
			gets++
			if gets >= 3 {
				if err := inner.Create(ctx, obj.DeepCopy()); err != nil {
					return err
				}
			}
			return inner.Get(ctx, key, out, opts...)
		},
	})
	c := &CacheClient{Client: wrapped, scheme: runtime.NewScheme()}
	if err := corev1.AddToScheme(c.scheme); err != nil {
		t.Fatal(err)
	}

	if err := c.blockApply(context.Background(), nil, obj, nil); err != nil {
		t.Fatalf("create should succeed once the object appears, got %v", err)
	}
	if gets < 3 {
		t.Fatalf("expected polling before create, got %d gets", gets)
	}
}
