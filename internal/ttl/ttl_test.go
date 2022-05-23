package ttl

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testNodeWithTtl(name string, creationOffest *time.Duration, ttl time.Duration) *corev1.Node {
	var creationTimestamp *metav1.Time
	if creationOffest != nil {
		creationTimestamp = &metav1.Time{Time: time.Now().Add(*creationOffest)}
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				NodeTtlLabelKey: ttl.String(),
			},
			CreationTimestamp: *creationTimestamp,
		},
	}
}

func TestExpiredTtl(t *testing.T) {
	type testNode struct {
		name           string
		creationOffest time.Duration
		ttl            time.Duration
	}

	type test struct {
		name      string
		nodes     []testNode
		nodeNames []string
	}

	tests := []test{
		{
			name: "single node with ttl",
			nodes: []testNode{
				{
					name:           "single",
					creationOffest: (-5 * time.Minute),
					ttl:            1 * time.Minute,
				},
			},
			nodeNames: []string{"single"},
		},
		{
			name: "multiple nodes",
			nodes: []testNode{
				{
					name:           "first-expired",
					creationOffest: (-2 * time.Hour),
					ttl:            1 * time.Hour,
				},
				{
					name:           "second-not-expired",
					creationOffest: (-25 * time.Hour),
					ttl:            48 * time.Hour,
				},
				{
					name:           "third-expired",
					creationOffest: (-24 * 15 * time.Hour),
					ttl:            24 * 14 * time.Hour,
				},
				{
					name:           "fourth-expired",
					creationOffest: (-24 * 2 * time.Hour),
					ttl:            1 * time.Minute,
				},
			},
			nodeNames: []string{"third-expired", "fourth-expired", "first-expired"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			client := fake.NewSimpleClientset()
			for _, n := range tt.nodes {
				node := testNodeWithTtl(n.name, &n.creationOffest, n.ttl)
				_, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			nodes, err := ttlEvictionCandidates(ctx, client)
			require.NoError(t, err)
			require.Len(t, nodes, len(tt.nodeNames))
			for i, nodeName := range tt.nodeNames {
				require.Equal(t, nodeName, nodes[i].Name)
			}
		})
	}
}

func TestInvalidTtlLabelValue(t *testing.T) {
	ctx := context.TODO()
	client := fake.NewSimpleClientset()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "invalid",
			Labels: map[string]string{
				NodeTtlLabelKey: "foobar",
			},
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
	}
	_, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = ttlEvictionCandidates(ctx, client)
	require.EqualError(t, err, "could not parse ttl duration: foobar")
}

func TestMissingCreationTimestamp(t *testing.T) {
	ctx := context.TODO()
	client := fake.NewSimpleClientset()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "missing-timestamp",
			Labels: map[string]string{
				NodeTtlLabelKey: "5m",
			},
		},
	}
	_, err := client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	require.NoError(t, err)
	nodes, err := ttlEvictionCandidates(ctx, client)
	require.NoError(t, err)
	require.Empty(t, nodes)
}
