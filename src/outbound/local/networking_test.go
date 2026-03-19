package local

import (
	"fmt"
	"testing"

	"github.com/jake-landersweb/previewctl/src/domain"
)

func TestNetworkingAdapter_AllocatePorts(t *testing.T) {
	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{
			"backend": {Path: "apps/backend"},
			"web":     {Path: "apps/web"},
		},
		InfraServices: map[string]domain.InfraService{
			"redis": {Name: "redis", Image: "redis:7-alpine", Port: 6379},
		},
	}

	adapter := NewNetworkingAdapter(cfg)
	ports, err := adapter.AllocatePorts("feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All ports should be in the 61000-65000 range
	for name, port := range ports {
		if port < 61000 || port >= 65000 {
			t.Errorf("port for '%s' is %d, expected in [61000, 65000)", name, port)
		}
	}

	// Should have ports for all services and infra
	if _, ok := ports["backend"]; !ok {
		t.Error("expected port for 'backend'")
	}
	if _, ok := ports["web"]; !ok {
		t.Error("expected port for 'web'")
	}
	if _, ok := ports["redis"]; !ok {
		t.Error("expected port for 'redis'")
	}
}

func TestNetworkingAdapter_GetServiceURL(t *testing.T) {
	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{
			"backend": {Path: "apps/backend"},
		},
	}

	adapter := NewNetworkingAdapter(cfg)
	url, err := adapter.GetServiceURL("feat-auth", "backend")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// URL should be http://localhost:<port> with port in range
	ports, _ := adapter.AllocatePorts("feat-auth")
	expected := "http://localhost:" + itoa(ports["backend"])
	if url != expected {
		t.Errorf("expected '%s', got '%s'", expected, url)
	}
}

func TestNetworkingAdapter_GetServiceURL_Unknown(t *testing.T) {
	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{},
	}

	adapter := NewNetworkingAdapter(cfg)
	_, err := adapter.GetServiceURL("feat-auth", "unknown")
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
