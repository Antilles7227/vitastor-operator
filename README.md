# vitastor-operator
Kubernetes operator for Vitastor - software-defined block storage.

## Description
Vitastor is a distributed block SDS, direct replacement of Ceph RBD and internal SDS's of public clouds.

Vitastor is architecturally similar to Ceph which means strong consistency, primary-replication, symmetric clustering and automatic data distribution over any number of drives of any size with configurable redundancy (replication or erasure codes/XOR).

Vitastor targets primarily SSD and SSD+HDD clusters with at least 10 Gbit/s network, supports TCP and RDMA and may achieve 4 KB read and write latency as low as ~0.1 ms with proper hardware which is ~10 times faster than other popular SDS's like Ceph or internal systems of public clouds.

Operator are built with Kubebuilder - a framework for building Kubernetes APIs with CRDs.

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

Also you need an etcd cluster for Vitastor with some additional tuning:
* max-txn-ops should be at least 100000
* I recommend to increase quota-backend-bytes (especially for large clusters), in some cases you can reach quota limit and it will stop etcd cluster at all.

### Running on the cluster 
1. Create namespace for vitastor and ConfigMap with vitastor.conf:

```sh
kubectl apply -f deploy/000-vitastor-csi-namespace.yaml
kubectl apply -f deploy/001-vitastor-configmap-osd.yaml
```

2. Install CSI driver pods and CSI provisioner:

```sh
kubectl apply -f deploy/002-csi.yaml
```

3. Install Instances of Custom Resources:

```sh
kubectl apply -f deploy/003-vitastor-crd.yaml
```
	
4. Deploy the controller to the cluster (fix image name/tag if you want to use self-hosted images)

```sh
kubectl apply -f deploy/004-vitastor-operator-deployment.yaml
```

5. Check cluster manifest (e.g. to set proper number of monitors, set label and image names if need so) and apply it

```sh
kubectl apply -f deploy/005-sample-vitastor-cluster.yaml
```

6. Apply labels for nodes with disks (so operator can deploy agents to it and see empty disks on them)

```sh
kubectl label nodes <your-node-name> vitastor-node=true
```

7. Prepare your disks for OSD using Agent API (right now it can be done through `curl`, in future there will be kubectl plugin for operational things).

**Note:** If you are using NVMe disk bigger than 2TiB, it's recommended to use more than one OSD per disk (for better IO utilization). It not, you may omit field `osd_num` in `curl` request.

```sh
# Use IPs from output to prepare disks
kubectl get pods -n vitastor-system -o custom-columns=NAME:.metadata.name,IP:.status.podIP | grep vitastor-agent
# Make request for every disk you want to use for Vitastor
curl -XPOST -H 'Content-Type: application/json' -d '{"disk": "<disk path>", "osd_num": <number of OSD per disk>}' http://<AGENT IP>:8000/disk/prepare
```

8. Create Pool for PVs. You may change some options of that pool, e.g. redundancy scheme (`xor`, `replicated` or `ec`), number of PGs, failure domain etc. Check [Vitastor docs](https://git.yourcmc.ru/vitalif/vitastor/src/branch/master/docs/config/pool.en.md) for more info (not all options supported right now)

```sh
kubectl apply deploy/006-sample-vitastor-pool.yaml
```

That's it! Now you can use created pool as StorageClass for your PVCs


9. Optional: By default, operator create additional placement level `dc` (Datacenter).
You can label your nodes with `fd.vitastor.io/<placement level name>: <value>` and operator will link node to that failue domain (it will be useful if you use [complex failure domain](https://git.yourcmc.ru/vitalif/vitastor/src/branch/master/docs/config/pool.en.md#level_placement) )
```sh
kubectl label node <node_name> fd.vitastor.io/dc: "DCNAME"
```

## TODO

* kubectl plugin for operator&vitastor maintenance
* More options for cluster tuning
* Automatic decomission and replacing disks

## Contacts

If you have any questions about that Operator and/or Vitastor itself, feel free to join out Telegram chat: [@vitastor](https://t.me/vitastor)