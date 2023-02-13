package status

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetNodePoolReadyAndMinCountExisting(t *testing.T) {
	nodePools := []testNodePool{
		{
			name:  "foo",
			ready: 10,
			min:   22,
		},
		{
			name:  "bar",
			ready: 25,
			min:   1,
		},
		{
			name:  "nodePool",
			ready: 2,
			min:   100,
		},
		{
			name:  "baz",
			ready: 87,
			min:   35,
		},
	}
	status := mockClusterAutoscalerStatus(t, nodePools)
	for _, nodePool := range nodePools {
		ready, min, err := getNodePoolReadyAndMinCount(status, nodePool.name)
		require.NoError(t, err)
		require.Equal(t, nodePool.ready, ready)
		require.Equal(t, nodePool.min, min)
	}
}

func TestGetNodePoolReadyAndMinCountNotFound(t *testing.T) {
	nodePools := []testNodePool{
		{
			name:  "foo",
			ready: 10,
			min:   22,
		},
	}
	status := mockClusterAutoscalerStatus(t, nodePools)
	_, _, err := getNodePoolReadyAndMinCount(status, "bar")
	require.EqualError(t, err, "could not find status for node pool: bar")
}

func TestHasScaleDownCapacity(t *testing.T) {
	type test struct {
		name   string
		ready  int
		min    int
		isSafe bool
	}

	tests := []test{
		{
			name:   "safe to scale down node pool foo",
			ready:  2,
			min:    1,
			isSafe: true,
		},
		{
			name:   "not safe to scale down node pool bar",
			ready:  1,
			min:    1,
			isSafe: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, cp := range getNodePoolLabelKeys() {
				node, nodePoolName := getNodePoolNameAndNode(t, cp, "foobar")
				nodePool := testNodePool{
					name:  nodePoolName,
					ready: tt.ready,
					min:   tt.min,
				}
				status := mockClusterAutoscalerStatus(t, []testNodePool{nodePool})
				ok, err := HasScaleDownCapacity(status, node)
				require.NoError(t, err)
				require.Equal(t, tt.isSafe, ok)
			}
		})
	}
}

type testNodePool struct {
	name  string
	ready int
	min   int
}

func mockClusterAutoscalerStatus(t *testing.T, nodePools []testNodePool) string {
	t.Helper()

	status := `Cluster-autoscaler status at 2022-08-11 12:35:11.797051423 +0000 UTC:
	Cluster-wide:
	  Health:      Healthy (ready=10 unready=0 notStarted=0 longNotStarted=0 registered=10 longUnregistered=0)
	               LastProbeTime:      2022-08-11 12:35:11.782449164 +0000 UTC m=+935528.106132475
	               LastTransitionTime: 2022-08-08 10:28:18.652598604 +0000 UTC m=+668714.976282015
	  ScaleUp:     NoActivity (ready=10 registered=10)
	               LastProbeTime:      2022-08-11 12:35:11.782449164 +0000 UTC m=+935528.106132475
	               LastTransitionTime: 2022-08-08 11:57:06.468308057 +0000 UTC m=+674042.791991368
	  ScaleDown:   NoCandidates (candidates=0)
	               LastProbeTime:      2022-08-11 12:35:11.782449164 +0000 UTC m=+935528.106132475
	               LastTransitionTime: 2022-08-08 12:03:59.241031335 +0000 UTC m=+674455.564714746

	NodeGroups:`

	//nolint:gocritic // ignore
	for _, nodePool := range nodePools {
		//nolint:lll // ignore
		status = fmt.Sprintf(`%[1]s
	  Name:        %[2]s
	  Health:      Healthy (ready=%[3]d unready=0 notStarted=0 longNotStarted=0 registered=%[3]d longUnregistered=0 cloudProviderTarget=%[3]d (minSize=%[4]d, maxSize=0))
	               LastProbeTime:      2022-08-11 12:35:11.782449164 +0000 UTC m=+935528.106132475
	               LastTransitionTime: 2022-08-08 10:28:18.652598604 +0000 UTC m=+668714.976282015
	  ScaleUp:     NoActivity (ready=%[3]d cloudProviderTarget=%[3]d)
	               LastProbeTime:      2022-08-11 12:35:11.782449164 +0000 UTC m=+935528.106132475
	               LastTransitionTime: 2022-08-08 11:57:06.468308057 +0000 UTC m=+674042.791991368
	  ScaleDown:   NoCandidates (candidates=0)
	               LastProbeTime:      2022-08-11 12:35:11.782449164 +0000 UTC m=+935528.106132475
	               LastTransitionTime: 2022-08-08 12:03:59.241031335 +0000 UTC m=+674455.564714746

        `, status, nodePool.name, nodePool.ready, nodePool.min)
	}
	return status
}

func getNodePoolNameAndNode(t *testing.T, cp string, name string) (*corev1.Node, string) {
	t.Helper()

	switch cp {
	case AzureNodePoolLabelKey:
		nodePoolName := fmt.Sprintf("aks-%s-11272894-vmss", name)
		nodeName := fmt.Sprintf("%s000004", nodePoolName)
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Labels: map[string]string{
					AzureNodePoolLabelKey: name,
				},
			},
		}, nodePoolName
	case AWSNodePoolLabelKey:
		eksNodePoolName := fmt.Sprintf("dev-eks2-%s", name)
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ip-10-100-27-63.eu-west-1.compute.internal",
				Labels: map[string]string{
					AWSNodePoolLabelKey: eksNodePoolName,
				},
			},
		}, fmt.Sprintf("eks-%s-c8c2d2a8-2d51-8764-1776-0b3f58267273", eksNodePoolName)
	case KubemarkNodePoolLabelKey:
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					KubemarkNodePoolLabelKey: name,
				},
			},
		}, name
	default:
		t.Fatal("unknown key")
		return nil, ""
	}
}
