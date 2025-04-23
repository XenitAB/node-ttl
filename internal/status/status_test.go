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
		ready, min, err := getNodePoolReadyAndMinCount("v1.31.2", status, nodePool.name)
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
	_, _, err := getNodePoolReadyAndMinCount("v1.31.2", status, "bar")
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
				node, nodePoolName := getNodePoolNameAndNode(t, "v1.31.2", cp, "foobar")
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

	status := `time: 2025-04-22 14:29:08.360891242 +0000 UTC
autoscalerStatus: Running
clusterWide:
  health:
    status: Healthy
    nodeCounts:
      registered:
        total: 5
        ready: 5
        notStarted: 0
      longUnregistered: 0
      unregistered: 0
    lastProbeTime: "2025-04-22T14:29:08.360891242Z"
    lastTransitionTime: "2025-04-17T23:46:40.655271485Z"
  scaleUp:
    status: NoActivity
    lastProbeTime: "2025-04-22T14:29:08.360891242Z"
    lastTransitionTime: "2025-04-22T00:37:48.447964164Z"
  scaleDown:
    status: NoCandidates
    lastProbeTime: "2025-04-22T14:29:08.360891242Z"
    lastTransitionTime: "2025-04-22T00:48:01.870055554Z"
nodeGroups:`

	//nolint:gocritic // ignore
	for _, nodePool := range nodePools {
		//nolint:lll // ignore
		status = fmt.Sprintf(`%[1]s
- name: %[2]s
  health:
    status: Healthy
    nodeCounts:
      registered:
        total: %[3]d
        ready: %[3]d
        notStarted: 0
      longUnregistered: 0
      unregistered: 0
    cloudProviderTarget: %[3]d
    minSize: %[4]d
    maxSize: 10
    lastProbeTime: "2025-04-22T14:29:08.360891242Z"
    lastTransitionTime: "2025-04-17T23:46:40.655271485Z"`, status, nodePool.name, nodePool.ready, nodePool.min)
	}

	return status
}

func getNodePoolNameAndNode(t *testing.T, version string, cp string, name string) (*corev1.Node, string) {
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
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: version,
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
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: version,
				},
			},
		}, fmt.Sprintf("eks-%s-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", eksNodePoolName)
	case KubemarkNodePoolLabelKey:
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					KubemarkNodePoolLabelKey: name,
				},
			},
			Status: corev1.NodeStatus{
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: version,
				},
			},
		}, name
	default:
		t.Fatal("unknown key")
		return nil, ""
	}
}
