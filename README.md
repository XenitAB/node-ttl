# Node TTL

Enforces a time to live (TTL) on Kubernetes nodes and evicts nodes which have expired.

## Background

Some Kubernetes clusters may replace nodes at a rapid rate due to frequent version upgrades or large variations in resource scaling. Other Kuberentes clusters however may have nodes that can be running for long periods of time. For the most part this is totally fine there are however benefits in limiting how long nodes can run within the cluster.

* Long running nodes tend to experience issues more frequently than new nodes.
* Restarting nodes limits how long a Pod can run which limits issues and tests frequent restarts.
* Having Pods reschedule can optimize their node placement and decrease the node count.
* New mutations to Pods will not take affect until a Pod is recreated.

Node TTL requires all nodes which should be considered to be labeled with a TTL duration. A node which has expired its TTL will first be cordoned and then drained. When the node is fully drained all Pods, excluding those created by DaemonSet, will be removed from the node. If required a new node will be added to cover the removed compute resources.Having properly configured Pod Disruptions Budgets is very important when using Node TTL as without it a Deployment could be at risk to have zero replicas running. The drain process will respect any configured Pod Disruption Budget.

> Node TTL requires [Cluster Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler) to be present in the cluster.

When the node is fully cordoned and drained no new Pods will be scheduled to that Pod. The Cluster Autoscaler will eventually considered the Node unneeded as it will be underutilized. When this occurs the Cluster Autoscaler will remove the underlying VM from the cloud provider which will result in the removal of the Node from the cluster. It may take some time for the Cluster Autoscaler to consider the Node to be unneeded. This duration can be decreased by configuring `scale-down-unneeded-time`, but it will also have an affect on the scale down of Nodes outside of TTL.

## Installation

Easiest method to install Node TTL is with the [Helm Chart](./charts/node-ttl). Out of the box it requires no configuration, although there are setting that can be tuned.

```shell
kubectl create namespace node-ttl
helm upgrade --install --version <version> node-ttl oci://ghcr.io/xenitab/helm-charts/node-ttl
```

## Usage

When the Node TTL is installed all that is required is to label the nodes correctly. Each node that should be considered by Node TTL should have the label `xkf.xenit.io/node-ttl` where the value is the duration is the maximum life time of the node. The TTL value has to be a valid [duration string](https://pkg.go.dev/time#ParseDuration) with a valid unit. Valid time units are `ns`, `us`, `ms`, `s`, `m`, `h`. 

```yaml
apiVersion: v1
kind: Node
metadata:
  name: kind-worker
  labels:
    xkf.xenit.io/node-ttl: 24h
```

The following node will be considered for eviction after it has existed for more the 24 hours.

### Scale Down Disabled

The cluster autoscaler annotation to [disable scale down](https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/FAQ.md#how-can-i-prevent-cluster-autoscaler-from-scaling-down-a-particular-node) is also respected by Node TTL. A node with the annotation will not be considered for eviction due to TTL.

```yaml
apiVersion: v1
kind: Node
metadata:
  name: kind-worker
  labels:
    xkf.xenit.io/node-ttl: 24h
  annotation:
    cluster-autoscaler.kubernetes.io/scale-down-disabled: true
```

### Safe To Evict

A Node with a Pod annotated that it is [not safe to evict](https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/FAQ.md#what-types-of-pods-can-prevent-ca-from-removing-a-node) will not be considered for eviction due to TTL. This is useful in situations where long running Jobs need to run much longer than the TTL set on any Node. This way the Node will be kept running until the Job completes and then it can be evicted. Be careful using this feature as setting this annotation on all Pods will stop any TTL evictions.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: no-evict
  annotations:
    cluster-autoscaler.kubernetes.io/safe-to-evict: false
```

### Cluster Autoscaler Status

A node pool where the min count is equal to the current node count will node be scaled down by cluster autoscaler. Even if the node is completely unused and a scale down candidate. This is because the cluster austoscaler has to fulfill the minum count requirement. This is an issue for Node TTL as it relies on cluster autoscaler node removal to replace nodes. If a node in this case were to be cordoned and drained the node would get stuck forever without any Pods scheduled to it. In a perfect world cluster autoscaler would allow the node removal and create a new node or alternativly preemptivly add a new node to the node pool.

To mitigate this issue Node TTL will check that the node pool has capcity to scale down, by reading the status in the [cluster autoscalers status Config Map](https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/FAQ.md#what-events-are-emitted-by-ca). If the node pool min count is equal to the current node count the node will not be considered a candidate for eviction.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
