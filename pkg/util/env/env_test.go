package env

import (
	"os"
	"testing"
	"time"
)

func TestSetEnvs(t *testing.T) {
	var st struct {
		DataDir             string        `env:"DATA_DIR"`
		RequiredClusterSize int           `env:"REQUIRED_CLUSTER_SIZE"`
		HealthCheckInterval time.Duration `env:"HEALTH_CHECK_INTERVAL"`
	}
	if err := os.Setenv("DATA_DIR", "data"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("REQUIRED_CLUSTER_SIZE", "5"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("HEALTH_CHECK_INTERVAL", "30s"); err != nil {
		t.Fatal(err)
	}
	if err := SetEnvs(&st); err != nil {
		t.Fatal(err)
	}
	if st.DataDir != "data" {
		t.Fatalf("incorrect string value: %v", st.DataDir)
	}
	if st.RequiredClusterSize != 5 {
		t.Fatalf("incorrect int value: %v", st.RequiredClusterSize)
	}
	if st.HealthCheckInterval != 30*time.Second {
		t.Fatalf("incorrect time.Duration value: %v", st.HealthCheckInterval)
	}
}
