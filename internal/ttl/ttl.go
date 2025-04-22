package ttl

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/avast/retry-go"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/xenitab/node-ttl/internal/status"
)

var evictedNodesTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "node_ttl_evicted_nodes_total",
	Help: "Total number of nodes that have been evicted due to TTL.",
})

var lastEvictionTimeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "node_ttl_last_eviction_timestamp_seconds",
	Help: "The date at which the last successful eviction occurred. Expressed as a Unix Epoch Time.",
})

const (
	//nolint:staticcheck // ignore this
	NodeTtlLabelKey      = "xkf.xenit.io/node-ttl"
	ScaleDownDisabledKey = "cluster-autoscaler.kubernetes.io/scale-down-disabled"
	PodSafeToEvictKey    = "cluster-autoscaler.kubernetes.io/safe-to-evict"
)

// nodeContainsNotSafeToEvictPods checks if a node has any Pods which are not safe to evict.
func nodeContainsNotSafeToEvictPods(ctx context.Context, client kubernetes.Interface, nodeName string) (bool, error) {
	opts := metav1.ListOptions{FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName)}
	podList, err := client.CoreV1().Pods("").List(ctx, opts)
	if err != nil {
		return false, err
	}
	for i := range podList.Items {
		pod := podList.Items[i]
		//nolint:staticcheck // ignore this
		if value, ok := pod.ObjectMeta.Annotations[PodSafeToEvictKey]; ok && value == "false" {
			return true, nil
		}
	}
	return false, nil
}

// nodeHasExpired returns true if node age is larger than ttl.
func nodeHasExpired(node *corev1.Node) (bool, error) {
	// Skip node which has not yet a creating timestamp
	nullTime := time.Time{}
	//nolint:staticcheck // ignore this
	if node.CreationTimestamp.Time == nullTime {
		return false, nil
	}
	//nolint:staticcheck // ignore this
	ttlValue, ok := node.ObjectMeta.Labels[NodeTtlLabelKey]
	if !ok {
		return false, fmt.Errorf("could not find ttl label in node: %s", NodeTtlLabelKey)
	}
	ttlDuration, err := time.ParseDuration(ttlValue)
	if err != nil {
		return false, fmt.Errorf("could not parse ttl value: %s", ttlValue)
	}
	diff := time.Since(node.CreationTimestamp.Time)
	if diff < ttlDuration {
		return false, nil
	}
	return true, nil
}

// ttlEvictionCandidate returns the most appropriate node to be evicted.
// If the a node with expired TTL is being in progress of being evicted it will be returned.
//
//nolint:gocognit,cyclop //ignore
func ttlEvictionCandidate(ctx context.Context, client kubernetes.Interface,
	clusterAutoscalerStatus *types.NamespacedName) (*corev1.Node, bool, error) {
	log := logr.FromContextOrDiscard(ctx)

	// Get nodes with a set TTL value
	nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: NodeTtlLabelKey})
	if err != nil {
		return nil, false, err
	}

	var candidate *corev1.Node
	for i := range nodeList.Items {
		node := nodeList.Items[i]
		log := log.WithValues("node", node.Name)

		// Scale down disabled annotation
		//nolint:staticcheck // ignore this
		if value, ok := node.ObjectMeta.Annotations[ScaleDownDisabledKey]; ok && value == "true" {
			log.Info("skipping node with scale down disabled")
			continue
		}

		// Node has expired TTL
		expired, err := nodeHasExpired(&node)
		if err != nil {
			log.Error(err, "skipping node that could not be determined if it is expired")
			continue
		}
		if !expired {
			continue
		}

		// Node pool has capacity to scale down
		if clusterAutoscalerStatus != nil {
			getOpts := metav1.GetOptions{}
			caConfigMap, err := client.CoreV1().ConfigMaps(clusterAutoscalerStatus.Namespace).Get(ctx, clusterAutoscalerStatus.Name, getOpts)
			if err != nil {
				return nil, false, err
			}
			caStatus, ok := caConfigMap.Data["status"]
			if !ok {
				return nil, false, fmt.Errorf("could not find status in config map")
			}
			ok, err = status.HasScaleDownCapacity(caStatus, &node)
			if err != nil {
				return nil, false, err
			}
			if !ok {
				log.Info("skipping because node pool does not have capacity for scale down")
				continue
			}
		}

		// Pods in Nodes can't be evicted
		containsNotSafeToEvict, err := nodeContainsNotSafeToEvictPods(ctx, client, node.Name)
		if err != nil {
			return nil, false, err
		}
		if containsNotSafeToEvict {
			log.Info("skipping node containing pod marked not safe to evict")
			continue
		}

		// We should return early with node if it is eligible for eviction and already unschedulable.
		// TODO: Should there be a more specific way to determine eviction in progress?
		if node.Spec.Unschedulable {
			log.Info("continuing with node that is already being evicted")
			return &node, true, nil
		}
		// Skip node if current candidate is older.
		if candidate != nil && node.CreationTimestamp.After(candidate.CreationTimestamp.Time) {
			continue
		}
		candidate = &node
	}
	if candidate == nil {
		return nil, false, nil
	}
	return candidate, true, nil
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
			return fmt.Errorf("could not cordon node %s: %w", node.Name, err)
		}
		err = drain.RunNodeDrain(helper, node.Name)
		if err != nil {
			return fmt.Errorf("could not drain node %s: %w", node.Name, err)
		}
		// Wait for node to be deleted
		return nil
	}, retry.OnRetry(func(n uint, err error) {
		log.Error(err, "retrying drain due to error", "attempt", n)
	}), retry.Attempts(5), retry.Delay(1*time.Second))
	if err != nil {
		return err
	}
	return nil
}

// evictNextExpiredNode will attempt to evict the next expired node if one exists.
func evictNextExpiredNode(ctx context.Context, client kubernetes.Interface, clusterAutoscalerStatus *types.NamespacedName) error {
	log := logr.FromContextOrDiscard(ctx)
	log.Info("checking for node with expired ttl")
	node, ok, err := ttlEvictionCandidate(ctx, client, clusterAutoscalerStatus)
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
	evictedNodesTotal.Inc()
	lastEvictionTimeSeconds.Set(float64(time.Now().Unix()))
	return nil
}
