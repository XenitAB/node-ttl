package ttl

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/avast/retry-go"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
)

const (
	NodeTtlLabelKey      = "xkf.xenit.io/node-ttl"
	ScaleDownDisabledKey = "cluster-autoscaler.kubernetes.io/scale-down-disabled"
)

// ttlEvictionCandidate returns the most appropriate node to be evicted.
// If the a node with expired TTL is being in progress of being evicted it will be returned.
func ttlEvictionCandidate(ctx context.Context, client kubernetes.Interface) (*corev1.Node, bool, error) {
	log := logr.FromContextOrDiscard(ctx)

	opts := metav1.ListOptions{LabelSelector: NodeTtlLabelKey}
	nodeList, err := client.CoreV1().Nodes().List(ctx, opts)
	if err != nil {
		return nil, false, err
	}

	// Get nodes with expired TTL
	nodes := []corev1.Node{}
	//nolint:gocritic // ignore
	for _, node := range nodeList.Items {
		log := log.WithValues("node", node.Name)

		nullTime := time.Time{}
		if node.CreationTimestamp.Time == nullTime {
			log.Info("skipping node without creation timestamp")
			continue
		}
		if value, ok := node.ObjectMeta.Annotations[ScaleDownDisabledKey]; ok && value == "true" {
			log.Info("skipping node with disabled scale down")
			continue
		}
		ttlValue, ok := node.ObjectMeta.Labels[NodeTtlLabelKey]
		if !ok {
			log.Error(fmt.Errorf("key not found in map"), "ttl label not found")
			continue
		}
		ttlDuration, err := time.ParseDuration(ttlValue)
		if err != nil {
			log.Error(err, "could not parse ttl value", "ttlValue", ttlValue)
			continue
		}
		diff := time.Since(node.CreationTimestamp.Time)
		if diff < ttlDuration {
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, false, nil
	}

	// Sort nodes oldest to newest
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[j].CreationTimestamp.After(nodes[i].CreationTimestamp.Time)
	})

	// Return first node if any that is currently being evicted
	//nolint:gocritic // ignore
	for _, node := range nodes {
		// TODO: Should there be a more specific way to determine eviction in progress?
		if !node.Spec.Unschedulable {
			continue
		}
		return &node, true, nil
	}

	// Return oldest node as candidate
	return &nodes[0], true, nil
}

// evictNode cordons and drains the specified node.
func evictNode(ctx context.Context, client kubernetes.Interface, node *corev1.Node) error {
	log := logr.FromContextOrDiscard(ctx)
	helper := &drain.Helper{
		Ctx:                 ctx,
		Client:              client,
		Force:               true, // Evict orphaned DaemonSet Pods and Pods with a controller
		GracePeriodSeconds:  -1,   // Respect Pod termination grace period.
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		ErrOut:              io.Discard,
		Out:                 io.Discard,
		OnPodDeletedOrEvicted: func(pod *corev1.Pod, usingEviction bool) {
			log.Info("completed eviction", "pod", pod.Name)
		},
	}

	// Retry to avoid large delays when API server hickups occur.
	err := retry.Do(func() error {
		err := drain.RunCordonOrUncordon(helper, node, true)
		if err != nil {
			return err
		}
		err = drain.RunNodeDrain(helper, node.Name)
		if err != nil {
			return err
		}
		return nil
	}, retry.Attempts(5), retry.Delay(1*time.Second))
	if err != nil {
		return err
	}
	return nil
}

// evictNextExpiredNode will attempt to evict the next expired node if one exists.
func evictNextExpiredNode(ctx context.Context, client kubernetes.Interface) error {
	log := logr.FromContextOrDiscard(ctx)
	log.Info("checking for node with expired ttl")
	node, ok, err := ttlEvictionCandidate(ctx, client)
	if err != nil {
		return err
	}
	if !ok {
		log.Info("no node with expired ttl found")
		return nil
	}
	log.Info("evicting node with expired ttl", "node", node.Name)
	err = evictNode(ctx, client, node)
	if err != nil {
		return err
	}
	log.Info("eviction complete", "node", node.Name)
	return nil
}
