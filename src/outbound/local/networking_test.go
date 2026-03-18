package local

import (
	"fmt"
	"testing"

	"github.com/jake/previewctl/src/domain"
)

func TestNetworkingAdapter_AllocatePorts(t *testing.T) {
	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{
			"backend": {Path: "apps/backend", Port: 8000},
			"web":     {Path: "apps/web", Port: 3000},
		},
		Infrastructure: map[string]domain.InfraServiceConfig{
			"redis": {Image: "redis:7-alpine", Port: 6379},
		},
	}

	adapter := NewNetworkingAdapter(cfg)
	ports := adapter.AllocatePorts("feat-auth")

	offset := domain.AllocatePortOffset("feat-auth")
	if ports["backend"] != 8000+offset {
		t.Errorf("expected backend %d, got %d", 8000+offset, ports["backend"])
	}
	if ports["web"] != 3000+offset {
		t.Errorf("expected web %d, got %d", 3000+offset, ports["web"])
	}
	if ports["redis"] != 6379+offset {
		t.Errorf("expected redis %d, got %d", 6379+offset, ports["redis"])
	}
}

func TestNetworkingAdapter_GetServiceURL(t *testing.T) {
	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{
			"backend": {Path: "apps/backend", Port: 8000},
		},
	}

	adapter := NewNetworkingAdapter(cfg)
	url, err := adapter.GetServiceURL("feat-auth", "backend")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	offset := domain.AllocatePortOffset("feat-auth")
	expected := "http://localhost:" + itoa(8000+offset)
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
