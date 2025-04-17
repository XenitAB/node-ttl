package status

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	AzureNodePoolLabelKey    = "kubernetes.azure.com/agentpool"
	AWSNodePoolLabelKey      = "eks.amazonaws.com/nodegroup"
	KubemarkNodePoolLabelKey = "autoscaling.k8s.io/nodegroup"
)

func HasScaleDownCapacity(status string, node *corev1.Node) (bool, error) {
	nodePoolName, err := getNodePoolName(node)
	if err != nil {
		return false, err
	}
	ready, min, err := getNodePoolReadyAndMinCount(status, nodePoolName)
	if err != nil {
		return false, err
	}
	if ready <= min {
		return false, nil
	}
	return true, nil
}

func getNodePoolLabelKeys() []string {
	return []string{AzureNodePoolLabelKey, AWSNodePoolLabelKey, KubemarkNodePoolLabelKey}
}

func getNodePoolName(node *corev1.Node) (string, error) {
	for _, key := range getNodePoolLabelKeys() {
		//nolint:staticcheck // ignore this
		nodePoolName, ok := node.ObjectMeta.Labels[key]
		if !ok {
			continue
		}

		// Custom handling for different cloud provider is required because Cluster Autoscaler will use VMSS or ASG names for the pool name.
		switch key {
		case AzureNodePoolLabelKey:
			// Azure agent pool label will only give the pool name used when creating it in AKS. The pool name used in the CA
			// status is the name of the VMSS automatically created by AKS. The Node name will be the same as the VMSS name with
			// a unique instance suffix. The label fetching is only done to check for an AKS cluster. The Node name with the
			// suffix removed is instead used as the pool name.
			nodePoolName = node.Name[:strings.LastIndex(node.Name, "-vmss")+5]
		case AWSNodePoolLabelKey:
			// AWS will use the generated ASG name for the pool name in the CA. This value cannot be found in the Node metadata.
			// The name is however, predicatable as it will be the same as the EKS node pool name with an additional UUID as a
			// suffix. This is why the UUID regex has to be appended to the end.
			nodePoolName = fmt.Sprintf("eks-%s-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", nodePoolName)
		}
		return nodePoolName, nil
	}
	return "", fmt.Errorf("could not find node pool label in node: %s", node.Name)
}

func getNodePoolReadyAndMinCount(status, nodePoolName string) (int, int, error) {
	health, err := getNodePoolHealth(status, nodePoolName)
	if err != nil {
		return 0, 0, err
	}
	ready, min, err := getReadyAndMinCount(health)
	if err != nil {
		return 0, 0, err
	}
	return ready, min, nil
}

func getNodePoolHealth(status string, nodePoolName string) (string, error) {
	reg, err := regexp.Compile(fmt.Sprintf(`\s*Name:\s*%s\n\s*Health:\s*(.*)`, nodePoolName))
	if err != nil {
		return "", err
	}
	matches := reg.FindStringSubmatch(status)
	if len(matches) == 0 {
		return "", fmt.Errorf("could not find status for node pool: %s", nodePoolName)
	}
	if len(matches) != 2 {
		return "", fmt.Errorf("expected match list to be of length 2 not: %d", len(matches))
	}
	return matches[1], nil
}

func getReadyAndMinCount(health string) (int, int, error) {
	reg := regexp.MustCompile(`Healthy \(ready=(\d+).*minSize=(\d+)`)
	matches := reg.FindStringSubmatch(health)
	if len(matches) != 3 {
		return 0, 0, fmt.Errorf("expected match list to be of length 3: %d", len(matches))
	}
	ready, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, 0, fmt.Errorf("could not convert ready count to int: %w", err)
	}
	min, err := strconv.Atoi(matches[2])
	if err != nil {
		return 0, 0, fmt.Errorf("could not convert min count to int: %w", err)
	}
	return ready, min, nil
}
