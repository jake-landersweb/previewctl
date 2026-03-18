package domain

import (
	"strings"
	"testing"
)

func TestTopologicalSort_Simple(t *testing.T) {
	services := map[string]ServiceConfig{
		"web":     {Path: "apps/web", Port: 3000, DependsOn: []string{"backend"}},
		"backend": {Path: "apps/backend", Port: 8000},
	}

	order, err := TopologicalSort(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	backendIdx := indexOf(order, "backend")
	webIdx := indexOf(order, "web")
	if backendIdx >= webIdx {
		t.Errorf("backend should come before web, got order: %v", order)
	}
}

func TestTopologicalSort_Diamond(t *testing.T) {
	services := map[string]ServiceConfig{
		"a": {Path: "a", Port: 1000},
		"b": {Path: "b", Port: 2000, DependsOn: []string{"a"}},
		"c": {Path: "c", Port: 3000, DependsOn: []string{"a"}},
		"d": {Path: "d", Port: 4000, DependsOn: []string{"b", "c"}},
	}

	order, err := TopologicalSort(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(order) != 4 {
		t.Fatalf("expected 4 items, got %d", len(order))
	}
	if order[0] != "a" {
		t.Errorf("expected 'a' first, got '%s'", order[0])
	}
	if order[3] != "d" {
		t.Errorf("expected 'd' last, got '%s'", order[3])
	}
}

func TestTopologicalSort_CycleDetection(t *testing.T) {
	services := map[string]ServiceConfig{
		"a": {Path: "a", Port: 1000, DependsOn: []string{"b"}},
		"b": {Path: "b", Port: 2000, DependsOn: []string{"a"}},
	}

	_, err := TopologicalSort(services)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle error, got: %v", err)
	}
}

func TestTopologicalSort_NoDependencies(t *testing.T) {
	services := map[string]ServiceConfig{
		"a": {Path: "a", Port: 1000},
		"b": {Path: "b", Port: 2000},
		"c": {Path: "c", Port: 3000},
	}

	order, err := TopologicalSort(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 items, got %d", len(order))
	}
	// Should be alphabetically sorted since all have in-degree 0
	if order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("expected alphabetical order, got %v", order)
	}
}

func TestTopologicalSort_InfraDependency(t *testing.T) {
	// Services can depend on infrastructure (redis), which is not in the services map.
	// These deps should be ignored for sorting purposes.
	services := map[string]ServiceConfig{
		"backend": {Path: "apps/backend", Port: 8000, DependsOn: []string{"redis"}},
		"web":     {Path: "apps/web", Port: 3000, DependsOn: []string{"backend"}},
	}

	order, err := TopologicalSort(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("expected 2 items, got %d", len(order))
	}
	if order[0] != "backend" || order[1] != "web" {
		t.Errorf("expected [backend, web], got %v", order)
	}
}

func indexOf(slice []string, val string) int {
	for i, v := range slice {
		if v == val {
			return i
		}
	}
	return -1
}
