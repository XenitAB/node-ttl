package e2e

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestCapcityCheck(t *testing.T) {
	path := os.Getenv("KIND_KUBECONFIG")
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	require.NoError(t, err)
	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)

  require.Never(t, func() bool {
    nodeList, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "xkf.xenit.io/node-ttl"})
    require.NoError(t, err)
    for _, node := range nodeList.Items {
      t.Log("checking that node is not evicted", node.Name)
			// TODO: There should be a better way to check that eviction is due to node ttl
      return node.Spec.Unschedulable
    }
    return false
  }, 1 * time.Minute, 5 * time.Second)
}

func TestTTLEviction(t *testing.T) {
	path := os.Getenv("KIND_KUBECONFIG")
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	require.NoError(t, err)
	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)

	nodeList, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "xkf.xenit.io/node-ttl"})
	require.NoError(t, err)
	nodes := nodeList.Items
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[j].CreationTimestamp.After(nodes[i].CreationTimestamp.Time)
	})

	nodeNames := []string{}
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	t.Log("checking eviction of nodes", nodeNames)

	for _, node := range nodeList.Items {
		t.Log("waiting for node to be evicted due to TTL", node.Name)

		require.Eventually(t, func() bool {
			getNode, err := client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
			require.NoError(t, err)
			// TODO: There should be a better way to check that eviction is due to node ttl
			if !getNode.Spec.Unschedulable {
				return false
			}
			return true
		}, 1*time.Minute, 1*time.Second, "node should be evicted due to TTL")
		t.Log("node has been marked unschedulable by node ttl", node.Name)

		require.Eventually(t, func() bool {
			podList, err := client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{FieldSelector: fmt.Sprintf("spec.nodeName=%s", node.Name)})
			require.NoError(t, err)
			pods := testFilterDaemonset(podList.Items)
			if len(pods) != 0 {
				return false
			}
			return true
		}, 30*time.Second, 1*time.Second, "node should be drained")
		t.Log("node has been drained", node.Name)

		// TODO: Make sure only one node is beeing evicted at once

		require.Eventually(t, func() bool {
			_, err := client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
			if !apierrors.IsNotFound(err) {
				return false
			}
			return true
		}, 2*time.Minute, 1*time.Second, "node should be delted")
		t.Log("underutilized node has been deleted", node.Name)
	}
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
