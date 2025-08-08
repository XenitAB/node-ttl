package e2e

import (
	"context"
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

func TestTTLEviction(t *testing.T) {
	path := os.Getenv("KIND_KUBECONFIG")
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	require.NoError(t, err)
	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)

	nodeList, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "xkf.xenit.io/node-ttl,autoscaling.k8s.io/nodegroup=asg1"})
	require.NoError(t, err)
	nodes := nodeList.Items
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[j].CreationTimestamp.After(nodes[i].CreationTimestamp.Time)
	})

	nodeMap := getNodesMap(nodes)
	nodeNames := getNodeKeys(nodeMap)
	t.Log("checking eviction of nodes", nodeNames)

	// What we want to test now is that all the nodes eventually get replaced by new ones
	require.Eventually(t, func() bool {
		for _, name := range nodeMap {
			_, err := client.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				delete(nodeMap, name)
				t.Logf("node %s doesn't exist anymore, continuing with next one", name)
				continue
			}
		}
		return len(nodeMap) == 0
	}, 5*time.Minute, 5*time.Second, "all nodes should have been evicted and replaced by new nodes")

	nodeList, err = client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "xkf.xenit.io/node-ttl,autoscaling.k8s.io/nodegroup=asg1"})
	require.NoError(t, err)
	nodeMap = getNodesMap(nodeList.Items)
	nodeNames = getNodeKeys(nodeMap)
	t.Log("nodes after all nodes have been evicted", nodeNames)
}

func getNodesMap(nodes []corev1.Node) map[string]string {
	nodeNames := make(map[string]string)
	for _, node := range nodes {
		nodeNames[node.Name] = node.Name
	}
	return nodeNames
}

func getNodeKeys(m map[string]string) []string {
	nodeKeys := []string{}
	for _, node := range m {
		nodeKeys = append(nodeKeys, node)
	}
	return nodeKeys
}
