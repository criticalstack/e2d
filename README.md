# e2d

[![PkgGoDev](https://pkg.go.dev/badge/github.com/criticalstack/e2d)](https://pkg.go.dev/github.com/criticalstack/e2d)
[![Build Status](https://cloud.drone.io/api/badges/criticalstack/e2d/status.svg)](https://cloud.drone.io/criticalstack/e2d)

e2d is a command-line tool for deploying and managing etcd clusters, both in the cloud or on bare-metal. It also includes [e2db](https://github.com/criticalstack/e2d/tree/master/pkg/e2db), an ORM-like abstraction for working with etcd.

## Table of Contents

- [What is e2d](#what-is-e2d)
  - [Features](#features)
  - [Installation](#installation)
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

### Installation

The easiest way to install:

```sh
curl -sSfL https://raw.githubusercontent.com/criticalstack/e2d/master/scripts/install.sh | sh
```

Pre-built binaries are also available in [Releases](https://github.com/criticalstack/e2d/releases/latest). e2d is written in Go so it is also pretty simple to install via go:

```sh
go get github.com/criticalstack/e2d/cmd/e2d
```

Packages can also be installed from [packagecloud.io](https://packagecloud.io/criticalstack/public) (includes systemd service file).

Debian/Ubuntu:

```sh
curl -sL https://packagecloud.io/criticalstack/public/gpgkey | apt-key add -
apt-add-repository https://packagecloud.io/criticalstack/public/ubuntu
apt-get install -y e2d
```

Fedora:

```sh
dnf config-manager --add-repo https://packagecloud.io/criticalstack/public/fedora
dnf install -y e2d
```

### Getting started

Running a single-node cluster:

```sh
❯ e2d run
```

Configuration is made through a single yaml file passed to `e2d run` via the `--config/-c` flag:

```sh
❯ e2d run -c config.yaml
```

Multi-node clusters require the `requiredClusterSize` value be set with the desired size of the cluster. Each node must also either provide a seed of peers (via `initialPeers`):

```yaml
requiredClusterSize: 3
discovery:
  initialPeers:
    - 10.0.2.15:7980
    - 10.0.2.17:7980
```

or specify a method for [peer discovery](#peer-discovery):

```yaml
requiredClusterSize: 3
discovery:
  type: aws/autoscaling-group

  # optionally provide the name of the ASG, otherwise will default to detecting
  # the ASG for the EC2 instance
  matches:
    name: my-asg
```

The e2d configuration file uses Kubernetes-like API versioning, ensuring that compatbility is maintained through implicit conversions. If `apiVersion` and `kind` are not provided, the version for the e2d binary is presumed, otherwise an explicit version can be provided as needed:

```yaml
apiVersion: e2d.crit.sh/v1alpha1
kind: Configuration
...
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

| Method | Name |
| --- | --- |
| AWS Autoscaling Group | `aws/autoscaling-group` |
| AWS EC2 tags | `aws/tags` |
| Digital Ocean tags | `digitalocean/tags` |

For example, running a 3-node cluster in AWS where initial peers are found via ec2 tags:

```sh
❯ e2d run -c - <<EOT
requiredClusterSize: 3
discovery:
  type: aws/tags
  matches:
    Name: my-cluster
    Email: admin@example.com
EOT
```

which will match for any EC2 instance that has both of the provided tags.

### Snapshots

Periodic backups can be made of the entire database, and e2d automates both creating these snapshot backups, as well as, restoring them in the event of a disaster.


```yaml
snapshot:
  interval: 5m
  file: s3://bucket/snapshot.tar.gz
```

The `snapshot.file` url is parsed to determine the target storage and location. When e2d first starts up, the presence of a valid backup file at the provided URL indicates it should attempt to restore from this snapshot (however, this may change to require explicit configuration).

#### Compression

The internal database layout of etcd lends itself to being compressed. To enable gzip compression set `snapshot.compression` to true in your config file:

```yaml
snapshot:
  compression: true
```

#### Encryption

Snapshot storage options like S3 use TLS and offer encryption-at-rest, however, it is possible that encryption of the snapshot file itself might be needed. This is especially true for other storage options that do not offer these features. Enabling snapshot encryption is simply a flag in the e2d configuration file, however, must be combined with `caCert`/`caKey` since the encryption key itself is derived from the CA private key:

```yaml
caCert: /etc/kubernetes/pki/etcd/ca.crt
caKey: /etc/kubernetes/pki/etcd/ca.key
snapshot:
  encryption: true
```

The encryption being used is AES-256 in [CTR mode](https://en.wikipedia.org/wiki/Block_cipher_mode_of_operation#Counter_(CTR)), with message authentication provided by HMAC-512_256. This mode was used because the Go implementation of AES-GCM would require the entire snapshot to be in-memory, and CTR mode allows for memory efficient streaming.

It is possible to use compression alongside of encryption, however, it is important to note that because of the possibility of opening up side-channel attacks, compression is not performed before encryption. The nature of how strong encryption works causes the encrypted snapshot to not gain benefits from compression. So enabling snapshot compression with encryption will cause the gzip level to be set to `gzip.NoCompression`, meaning it still creates a valid gzip file, but doesn't waste nearly as many compute resources while doing so (it is used because the gzip header is useful for file-type detection and checksum validation).

```yaml
caCert: /etc/kubernetes/pki/etcd/ca.crt
caKey: /etc/kubernetes/pki/etcd/ca.key
snapshot:
  compression: true
  encryption: true
  interval: 5m
  file: s3://bucket/snapshot.tar.gz
```

#### Storage options

The `snapshot.file` value has several schemes it implicitly understands:

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
❯ e2d certs init
```

The remaining certificates for the client, peer, and server communication are created automatically and placed in the same directory as the CA key/cert.

### Running with systemd

This systemd service file is provided with the Debian/Ubuntu packages:

```
[Unit]
Description=e2d

[Service]
Environment="E2D_CONFIG_ARGS=--config=/etc/e2d.yaml"
ExecStart=/usr/local/bin/e2d run $E2D_CONFIG_ARGS
EnvironmentFile=-/etc/e2d.conf
Restart=on-failure
RestartSec=30

[Install]
WantedBy=multi-user.target
```

It relies on providing the e2d configuration in a fixed location: `/etc/e2d.yaml`. Environment variables can be set for the service by providing them in the `/etc/e2d.conf` file.

## FAQ

### Can e2d scale up (or down) after cluster initialization?

The short answer is No, because it is unsafe to scale etcd and any solution that scales etcd is increasing the chance of cluster failure. This is a feature that will be supported in the future, but it relies on new features and fixes to etcd. Some context will be necessary to explain why:

A common misconception about etcd is that it is scalable. While etcd is a distributed key/value store, the reason it is distributed is to provide for distributed consensus, *NOT* to scale in/out for performance (or flexibility). In fact, the best performing etcd cluster is when it only has 1 member and the performance goes down as more members are added. In etcd v3.4, a new type of member called learners was introduced. These are members that can receive raft log updates, but are not part of the quorum voting process. This will be an important feature for many reasons, like stability/safety and faster recovery from faults, but will also potentially<sup>[[1]](#faq-fn-1)</sup> enable etcd clusters of arbitrary sizes.

So why not scale within the [recommended cluster sizes](https://github.com/etcd-io/etcd/blob/master/Documentation/faq.md#what-is-maximum-cluster-size) if the only concern is performance? Previously, etcd clusters have been vulnerable to corruption during membership changes due to the way etcd implemented raft. This has only recently been addressed by incredible work from CockroachDB, and it is worth reading about the issue and the solution in this blog post: [Availability and Region Failure: Joint Consensus in CockroachDB](https://www.cockroachlabs.com/blog/joint-consensus-raft/).

The last couple features needed to safely scale have been roadmapped for v3.5 and are highlighted in the [etcd learner design doc](https://github.com/etcd-io/etcd/blob/master/Documentation/learning/design-learner.md#features-in-v35):

> Make learner state only and default: Defaulting a new member state to learner will greatly improve membership reconfiguration safety, because learner does not change the size of quorum. Misconfiguration will always be reversible without losing the quorum.

> Make voting-member promotion fully automatic: Once a learner catches up to leader’s logs, a cluster can automatically promote the learner. etcd requires certain thresholds to be defined by the user, and once the requirements are satisfied, learner promotes itself to a voting member. From a user’s perspective, “member add” command would work the same way as today but with greater safety provided by learner feature.

Since we want to implement this feature as safely and reliably as possible, we are waiting for this confluence of features to become stable before finally implementing scaling into e2d.

<a name="faq-fn-1">[1]</a> Only potentially, because the maximum is currently set to allow only 1 learner. There is a concern that too many learners could have a negative impact on the leader which is discussed briefly [here](https://github.com/etcd-io/etcd/issues/11401). It is also worth noting that other features may also fulfill the same need like some kind of follower replication: [etcd#11357](https://github.com/etcd-io/etcd/issues/11357).
