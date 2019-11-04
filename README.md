# e2d

[![GoDoc](https://godoc.org/github.com/criticalstack/e2d?status.svg)](https://godoc.org/github.com/criticalstack/e2d)
[![Build Status](https://cloud.drone.io/api/badges/criticalstack/e2d/status.svg)](https://cloud.drone.io/criticalstack/e2d)

e2d is a command-line tool for deploying and managing etcd clusters, both in the cloud or on bare-metal. It also includes [e2db](https://github.com/criticalstack/e2d/tree/master/pkg/e2db), an ORM-like abstraction for working with etcd.

## Table of Contents

- [What is e2d](#what-is-e2d)
  - [Features](#features)
  - [Design](#design)
- [Getting started](#getting-started)
  - [Required ports](#required-ports)
- [Configuration](#configuration)
  - [Peer discovery](#peer-discovery)
  - [Snapshots](#snapshots)
    - [Compression](#compression)
    - [Encryption](#encryption)
    - [Storage options](#storage-options)
- [Usage](#usage)
  - [Generating certificates](#generating-certificates)
  - [Running with systemd](#running-with-systemd)
  - [Running with Kubernetes](#running-with-kubernetes)
- [FAQ](#faq)

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

Multi-node clusters require that the `--required-cluster-size/-n` flag be set with the desired size of the cluster. Each node must also either provide a seed of peers (via the `--bootstrap-addrs` flag):

```bash
$ e2d run -n 3 --bootstrap-addrs 10.0.2.15,10.0.2.17
```

or specify a method for [peer discovery](#peer-discovery):

```bash
$ e2d run -n 3 --peer-discovery aws-autoscaling-group
```

### Required ports

The same ports required by etcd are necessary, along with a couple new ones:

| Port | Description |
| --- | --- |
| TCP/2379 | Required by etcd. Used for incoming client connections. This port should allow ingress from any system that will need to connect to etcd as a client. |
| TCP/2380 | Required by etcd. Used by etcd members for internal etcd cluster communication. This port should be allowed only for systems that will be running e2d. |
| UDP/7980 | Required by memberlist. Used for node-liveness checks and sharing membership data. |
| TCP/7980 | Required by memberlist. Used for node-liveness checks and sharing membership data. |

*Note: Hashicorp's [memberlist](https://github.com/hashicorp/memberlist) requires both TCP and UDP for port 7980 to allow memberlist to fully communicate.*

## Configuration

### Peer discovery

Peers can be automatically discovered based upon several different built-in methods:

| Method | Usage |
| --- | --- |
| AWS Autoscaling Group | `aws-autoscaling-group` |
| AWS EC2 tags | `ec2-tags[:<name>=<value>,<name>=<value>]` |
| Digital Ocean tags | `do-tags[:<value>,<value>]` |

For example, running a 3-node cluster in AWS where initial peers are found via ec2 tags:

```bash
$ e2d run -n 3 --peer-discovery ec2-tags:Name=my-cluster,Email=admin@example.com
```

which will match for any EC2 instance that has both of the provided tags.

### Snapshots

Periodic backups can be made of the entire database, and e2d automates both creating these snapshot backups, as well as, restoring them in the event of a disaster.

Getting started with periodic snapshots only requires passing a file location to `--snapshot-backup-url`. The url is then parsed to determine the target storage and location. When e2d first starts up, the presence of a valid backup file at the provided URL indicates it should attempt to restore from this snapshot.

#### Compression

The internal database layout of etcd lends itself to being compressed. This is why e2d allows for snapshots to be compressed in-memory at the time of creation. To enable gzip compression, use the `--snapshot-compression` flag.

#### Encryption

Snapshot storage options like S3 use TLS and offer encryption-at-rest, however, it is possible that encryption of the snapshot file itself might be needed. This is especially true for other storage options that do not offer these features. Enabling snapshot encryption is simply `--snapshot-encryption`. The encryption key itself is derived only from the CA private key, so enabling encryption also requires passing `--ca-key <key path>`.

The encryption being used is AES-256 in [CTR mode](https://en.wikipedia.org/wiki/Block_cipher_mode_of_operation#Counter_(CTR)), with message authentication provided by HMAC-512_256. This mode was used because the Go implementation of AES-GCM would require the entire snapshot to be in-memory, and CTR mode allows for memory efficient streaming.

It is possible to use compression alongside of encryption, however, it is important to note that because of the possibility of opening up side-channel attacks, compression is not performed before encryption. The nature of how strong encryption works causes the encrypted snapshot to not gain benefits from compression. So enabling snapshot compression with encryption will cause the gzip level to be set to `gzip.NoCompression`, meaning it still creates a valid gzip file, but doesn't waste nearly as many compute resources while doing so.

#### Storage options

The `--snapshot-backup-url` has several schemes it implicitly understands:

| Storage Type | Usage |
| --- | --- |
| File | `file://<path>` |
| AWS S3 | `s3://<bucket>[/path]` |
| Digital Ocean Spaces | `https://<region>.digitaloceanspaces.com/<bucket>[/path]` |


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

An example unit file for running via systemd in an AWS ASG:

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
  --peer-discovery=aws-autoscaling-group \
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
    - --peer-discovery=aws-autoscaling-group
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
        - --peer-discovery=aws-autoscaling-group
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
