package status

import (
	"fmt"
	"regexp"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

const (
	AzureNodePoolLabelKey    = "kubernetes.azure.com/agentpool"
	AWSNodePoolLabelKey      = "eks.amazonaws.com/nodegroup"
	KubemarkNodePoolLabelKey = "autoscaling.k8s.io/nodegroup"
)

func HasScaleDownCapacity(status string, node *corev1.Node) (bool, error) {
	nodePool, err := getNodePoolName(node)
	if err != nil {
		return false, err
	}
	health, err := getNodePoolHealth(status, nodePool)
	if err != nil {
		return false, err
	}
	ready, min, err := getReadyAndMinCount(health)
	if err != nil {
		return false, err
	}
	if ready <= min {
		return false, nil
	}
	return true, nil
}

func getNodePoolName(node *corev1.Node) (string, error) {
	labelKeys := []string{AzureNodePoolLabelKey, AWSNodePoolLabelKey, KubemarkNodePoolLabelKey}
	for _, key := range labelKeys {
		nodePool, ok := node.ObjectMeta.Labels[key]
		if !ok {
			continue
		}
		return nodePool, nil
	}
	return "", fmt.Errorf("could not find node pool label in node: %s", node.Name)
}

func getNodePoolHealth(status string, nodePool string) (string, error) {
	reg := regexp.MustCompile(`\s*Name:\s*(.*)\n\s*Health:\s*(.*)`)
	matches := reg.FindAllStringSubmatch(status, -1)
	for _, match := range matches {
		if len(match) != 3 {
			return "", fmt.Errorf("expected match list to be of length 3: %d", len(match))
		}
		if match[1] != nodePool {
			continue
		}
		return match[2], nil
	}
	return "", fmt.Errorf("could not find status for node pool: %s", nodePool)
}

func getReadyAndMinCount(health string) (int, int, error) {
	reg := regexp.MustCompile(`Healthy \(ready=(\d).*minSize=(\d)`)
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
