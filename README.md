# e2d

[![GoDoc](https://godoc.org/github.com/criticalstack/e2d?status.svg)](https://godoc.org/github.com/criticalstack/e2d)
[![Build Status](https://cloud.drone.io/api/badges/criticalstack/e2d/status.svg)](https://cloud.drone.io/criticalstack/e2d)

e2d is a command-line tool for deploying and managing etcd clusters, both in the cloud or on bare-metal. It also includes [e2db](https://github.com/criticalstack/e2d/tree/master/pkg/e2db), an ORM-like abstraction for working with etcd.

## Table of Contents

- [What is e2d](#what-is-e2d)
  - [Features](#features)
  - [Design](#design)
- [Getting started](#getting-started)
- [Configuration](#configuration)
  - [Providers](#providers)
    - [Storage options](#storage-options)
- [Usage](#usage)
  - [Generating certificates](#generating-certificates)
  - [Running with systemd](#running-with-systemd)
  - [Running with Kubernetes](#running-with-kubernetes)

## What is e2d

e2d is designed to manage highly available etcd clusters in the cloud. It can be configured to interact directly with your cloud provider to seed the cluster membership and backup/restore etcd data.

### Features

 * Cluster management

   Membership and node liveness are determined with the help of a gossip network ([memberlist](https://github.com/hashicorp/memberlist)). This enables the automation of many administrative tasks, such as adding/removing members to the cluster in the event of failure. This is also how e2d is able to bootstrap new clusters in automated deployments.
 * Disaster recovery
   
   Periodic snapshots can be made and saved directly to different types of [storage](#storage-options). This allows e2d to automatically restore the cluster, even from total cluster failure.
 * Minimal configuration
   
   Much of the etcd configuration is handled, simplifying the configuration of e2d. e2d automatically differentiates between runtime configurations, such as an existing node restarting, removing dead nodes to attempt rejoining the cluster, establishing a new cluster, restoring a cluster from snapshot, etc.
 * Peer discovery
   
   In dynamic cloud environments, etcd membership is seeded from cloud provider APIs and maintained via a gossip network. This ensures etcd stays healthy and available when nodes might be automatically created or destroyed.

### Design

A key design philosophy of e2d is ease-of-use, so having minimal and/or automatic configuration is an important part of the user experience. Since e2d uses a gossip network for peer discovery, cloud metadata services can be leveraged to dynamically create the initial etcd bootstrap configuration. The gossip network is also used for determining node liveness, ensuring that healthy members can safely (and automatically) remove and replace a failing minority of members. An automatic snapshot feature creates periodic backups, which can be restored in the case of a majority failure of etcd members. This is all handled for the user by simply setting a shared file location for the snapshot, and e2d handles the rest. This ends up being incredibly helpful for those using Kubernetes in their dev environments, where cost-savings policies might stop instances over nights/weekends.

While e2d makes use of cloud provider specific features, it never depends on them. The abstraction for peer discovery and snapshot storage are generalized so they can be ported to many different platforms trivially. Another neat aspect of e2d is that it embeds etcd directly into its own binary, effectively using it like a library. This is what enables some of the more complex automation, and with only one binary it reduces the complexity of deploying into production.

## Getting started

Running a single-node cluster:

```bash
$ e2d run
```

Multi-node clusters require that the `--required-cluster-size/-n` flag be set with the desired size of the cluster. Each node must also either provide a seed of peers (via the `--bootstrap-addrs` flag) or specify a [provider](#providers):

```bash
$ e2d run -n 3 --provider=aws
```

## Configuration

### Providers

#### Storage options

## Usage

e2d should be managed by your service manager. The following templates should get you started.
Note: these examples assume e2d is being deployed in AWS.

### Generating certificates

Mutual TLS authentication is highly recommended and e2d embeds the necessary functionality to generate the required key pairs. To get started with a new e2d cluster, first initialize a new key/cert pair:

```bash
$ e2d pki init
```

Then create the remaining certificates for the client, peer, and server communication:

```bash
$ e2d pki gencerts
```

This will create the remaining key pairs needed to run e2d based on the initial cluster key pair.

### Running with systemd

An example unit file for running via systemd in AWS:

```
[Unit]
Description=e2d

[Service]
ExecStart=/usr/local/bin/e2d run \
  --data-dir=/var/lib/etcd \
  --ca-cert=/etc/kubernetes/pki/etcd/ca.crt \
  --client-cert=/etc/kubernetes/pki/etcd/client.crt \
  --client-key=/etc/kubernetes/pki/etcd/client.key \
  --peer-cert=/etc/kubernetes/pki/etcd/peer.crt \
  --peer-key=/etc/kubernetes/pki/etcd/peer.key \
  --server-cert=/etc/kubernetes/pki/etcd/server.crt \
  --server-key=/etc/kubernetes/pki/etcd/server.key \
  --provider=aws \
  --required-cluster-size=3 \
  --snapshot-backup-url=s3://e2d_snapshot_bucket
Restart=on-failure
RestartSec=30

[Install]
WantedBy=multi-user.target
```

### Running in Kubernetes

e2d can be run within kubernetes either as a static pod (as a replacement for etcd in kubeadm installs) or a daemonset. 

#### Static pod

Drop this template in your static pod directory (likely `/etc/kubernetes/manifests`) to have Kubelet run the process:

```
# /etc/kubernetes/manifests/e2d.yaml
# run e2d as a static pod
apiVersion: v1
kind: Pod
metadata:
  annotations:
    scheduler.alpha.kubernetes.io/critical-pod: ""
  labels:
    component: e2d
    tier: control-plane
  name: e2d
  namespace: kube-system
spec:
  containers:
  - command:
    - /e2d
    - run
    - --data-dir=/data
    - --ca-cert=/certs/ca.crt
    - --client-cert=/certs/client.crt
    - --client-key=/certs/client.key
    - --peer-cert=/certs/peer.crt
    - --peer-key=/certs/peer.key
    - --server-cert=/certs/server.crt
    - --server-key=/certs/server.key
    - --provider=aws
    - --required-cluster-size=3
    - --snapshot-backup-url=s3://e2d_snapshot_bucket
    image: criticalstack/e2d
    imagePullPolicy: IfNotPresent
    name: e2d
    volumeMounts:
    - mountPath: /data
      name: data
    - mountPath: /certs
      name: certs
  hostNetwork: true
  priorityClassName: system-cluster-critical
  volumes:
  - hostPath:
      path: /etc/kubernetes/pki/etcd
      type: DirectoryOrCreate
    name: certs
  - hostPath:
      path: /var/lib/etcd
      type: DirectoryOrCreate
    name: data
status: {}
```

#### Daemonset

e2d expects to run one-per-node so it may also be used as a daemonset:

```
# Run e2d as a daemonset
---
kind: DaemonSet
apiVersion: extensions/v1beta1
metadata:
  annotations:
    scheduler.alpha.kubernetes.io/critical-pod: ""
  labels:
    component: e2d
    tier: control-plane
  name: e2d
  namespace: kube-system
spec:
  template:
    metadata:
      labels:
        k8s-app: e2d
        name: e2d
    spec:
      containers:
      - command:
        - /e2d
        - run
        - --ca-cert=/certs/ca.crt
        - --data-dir=/data
        - --peer-cert=/certs/peer.crt
        - --peer-key=/certs/peer.key
        - --provider=aws
        - --required-cluster-size=3
        - --server-cert=/certs/server.crt
        - --server-key=/certs/server.key
        - --snapshot-backup-url=s3://e2d_snapshot_bucket
        image: criticalstack/e2d
        imagePullPolicy: IfNotPresent
        name: e2d
        volumeMounts:
        - mountPath: /data
          name: data
        - mountPath: /certs
          name: certs
      hostNetwork: true
      priorityClassName: system-cluster-critical
      volumes:
      - hostPath:
          path: /path/to/certs
          type: DirectoryOrCreate
        name: certs
      - hostPath:
          path: /var/lib/etcd
          type: DirectoryOrCreate
        name: data
```

