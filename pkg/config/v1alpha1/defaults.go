package v1alpha1

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime"
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

func SetDefaults_Configuration(obj *Configuration) {
	if obj.DataDir == "" {
		obj.DataDir = "data"
	}
	if obj.RequiredClusterSize == 0 {
		obj.RequiredClusterSize = 1
	}
	if obj.ClientAddr.Host == "" {
		obj.ClientAddr.Host = "0.0.0.0"
	}
	if obj.ClientAddr.Port == 0 {
		obj.ClientAddr.Port = DefaultClientPort
	}
	if obj.PeerAddr.Host == "" {
		obj.PeerAddr.Host = "0.0.0.0"
	}
	if obj.PeerAddr.Port == 0 {
		obj.PeerAddr.Port = DefaultPeerPort
	}
	if obj.GossipAddr.Host == "" {
		obj.GossipAddr.Host = "0.0.0.0"
	}
	if obj.GossipAddr.Port == 0 {
		obj.GossipAddr.Port = DefaultGossipPort
	}
	if obj.HealthCheckInterval.Duration == 0 {
		obj.HealthCheckInterval.Duration = 1 * time.Minute
	}
	if obj.HealthCheckTimeout.Duration == 0 {
		obj.HealthCheckTimeout.Duration = 5 * time.Minute
	}
	if obj.DiscoveryConfiguration.BootstrapTimeout.Duration == 0 {
		obj.DiscoveryConfiguration.BootstrapTimeout.Duration = 30 * time.Minute
	}
	if obj.SnapshotConfiguration.Interval.Duration == 0 {
		obj.SnapshotConfiguration.Interval.Duration = 1 * time.Minute
	}
	if obj.DiscoveryConfiguration.ExtraArgs == nil {
		obj.DiscoveryConfiguration.ExtraArgs = make(map[string]string)
	}
	if obj.EtcdLogLevel == "" {
		obj.EtcdLogLevel = "error"
	}
	if obj.MemberlistLogLevel == "" {
		obj.MemberlistLogLevel = "error"
	}
	if obj.MetricsConfiguration.Type == "" {
		obj.MetricsConfiguration.Type = MetricsBasic
	}
}
