# Node TTL

Enforces a time to live (TTL) on Kubernetes nodes and evicts nodes which have expired.

## Background

Some Kubernetes clusters may replace nodes at a rapid rate due to frequent version upgrades or large variations in resource scaling. Other Kuberentes clusters however may have nodes that can be running for long periods of time. For the most part this is totally fine ther are however benefits in limiting how long nodes can run within the cluster.

* Long running nodes tend to experience issues more frequently than fresh ones.
* Restarting nodes limits how long a pod can run which tests pods for frequest restarts.
* Having Pods reschedule can optimize their node placement and descrease the node count.
* New mutations to pods will not take affect until a pod is recreated.

Node TTL requires all nodes which should be considered to be labeld with a TTL duration. A node which has expired its TTL will first be cordoned and then drained. When the node is fully drained all pods, excluding those created by daemonset, will be removed from the node. If required a new node will be added to cover the removed compute resources.Having properly configured Pod Disruptions Budgets is very important when using Node TTL as without it a Deployment could be at risk to have zero replicas running. The drain process will respect any configured Pod Disruption Budget.

> Node TTL requires [Cluster Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler) to be present in the cluster.

When the node is fully cordoned and drained no new Pods will be scheduled to that Pod. The Cluster Autoscaler will eventually considered the Node unneeded as it will be underutlizied. When this occurs the Cluster Autoscaler will remove the underlying VM from the cloud provider which will result in the removal of the Node from the cluster. It may take some time for the Cluster Autoscaler to consider the Node to be unneeded. This duration can be decreased by configuring `scale-down-unneeded-time`, but it will also have an affect on the scale down of Nodes outside of TTL.

## How To

```shell
helm repo add
helm install
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
