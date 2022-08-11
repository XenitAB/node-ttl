package status

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const clusterStatus = `Cluster-autoscaler status at 2022-08-11 12:35:11.797051423 +0000 UTC:
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

func TestBasic(t *testing.T) {
	type nodePool struct {
		name  string
		ready int
		min   int
	}

	type test struct {
		name      string
		nodePools []nodePool
		nodePool  string
		safe      bool
	}

	tests := []test{
		{
			name: "safe to scale down node pool foo",
			nodePools: []nodePool{
				{
					name:  "foo",
					ready: 2,
					min:   1,
				},
			},
			nodePool: "foo",
			safe:     true,
		},
		{
			name: "not safe to scale down node pool bar",
			nodePools: []nodePool{
				{
					name:  "foo",
					ready: 1,
					min:   1,
				},
				{
					name:  "bar",
					ready: 25,
					min:   25,
				},
			},
			nodePool: "bar",
			safe:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := clusterStatus
			for _, nodePool := range tt.nodePools {
				//nolint:lll //ignore
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
			node := corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.nodePool,
					Labels: map[string]string{
						AzureNodePoolLabelKey: tt.nodePool,
					},
				},
			}
			ok, err := HasScaleDownCapacity(status, &node)
			require.NoError(t, err)
			require.Equal(t, tt.safe, ok)
		})
	}
}
