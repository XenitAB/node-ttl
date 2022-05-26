package e2e

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/xenitab/node-ttl/internal/ttl"
)

func TestBasic(t *testing.T) {
	ctx := context.TODO()

	path := os.Getenv("E2E_KUBECONFIG")
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	require.NoError(t, err)
	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)

	allNodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: ttl.NodeTtlLabelKey})
	require.NoError(t, err)

	nodes := nodeList.Items
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[j].CreationTimestamp.After(nodes[i].CreationTimestamp.Time)
	})
	for _, node := range nodeList.Items {
		fmt.Println("checking node", node.Name)
    for {
			// Wait for node to become tainted
			getNode, err := client.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
			require.NoError(t, err)
			if !getNode.Spec.Unschedulable {
				continue
			}
			// Check that the node has been drained
			podList, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: fmt.Sprintf("spec.nodeName=%s", node.Name)})
			require.NoError(t, err)
			pods := testFilterDaemonset(podList.Items)
			if len(pods) != 0 {
				continue
			}
			// Fake cluster autoscaler and remove node
			err = client.CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{})
			require.NoError(t, err)
			break
		}
	}

	expectedNodeCount := len(allNodeList.Items) - len(nodeList.Items)
	remainderNodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Equal(t, expectedNodeCount, len(remainderNodeList.Items))
}

func testFilterDaemonset(pods []corev1.Pod) []corev1.Pod {
	filteredPods := []corev1.Pod{}
OUTER:
	for _, pod := range pods {
		for _, ownerRef := range pod.OwnerReferences {
			if ownerRef.APIVersion == "apps/v1" && ownerRef.Kind == "DaemonSet" {
				continue OUTER
			}
		}
		filteredPods = append(filteredPods, pod)
	}
	return filteredPods
}
