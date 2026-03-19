package domain

import "testing"

func TestAllocatePortBlock_Deterministic(t *testing.T) {
	names := []string{"backend", "web", "redis"}

	ports1, err := AllocatePortBlock("feat-auth", names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ports2, err := AllocatePortBlock("feat-auth", names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range names {
		if ports1[name] != ports2[name] {
			t.Errorf("expected deterministic port for '%s', got %d and %d", name, ports1[name], ports2[name])
		}
	}
}

func TestAllocatePortBlock_InRange(t *testing.T) {
	names := []string{"backend", "web", "redis"}
	envNames := []string{
		"feat-auth", "feat-payments", "bugfix-login", "main",
		"release-v2", "hotfix-42", "test-env", "dev",
		"a", "very-long-environment-name-that-should-still-work",
	}

	for _, envName := range envNames {
		ports, err := AllocatePortBlock(envName, names)
		if err != nil {
			t.Fatalf("unexpected error for '%s': %v", envName, err)
		}
		for svc, port := range ports {
			if port < portRangeStart || port >= portRangeEnd {
				t.Errorf("port for '%s' in env '%s' is %d, expected [%d, %d)", svc, envName, port, portRangeStart, portRangeEnd)
			}
		}
	}
}

func TestAllocatePortBlock_DifferentEnvNames(t *testing.T) {
	names := []string{"backend"}

	ports1, err := AllocatePortBlock("feat-auth", names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ports2, err := AllocatePortBlock("feat-payments", names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Not guaranteed to be different for all pairs, but these two should differ
	// due to FNV-1a properties.
	if ports1["backend"] == ports2["backend"] {
		t.Logf("warning: ports for 'feat-auth' and 'feat-payments' collide at %d", ports1["backend"])
	}
}

func TestAllocatePortBlock_SequentialPorts(t *testing.T) {
	names := []string{"a", "b", "c"}

	ports, err := AllocatePortBlock("test-env", names)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Names are sorted, so ports should be sequential (assuming all are free)
	if ports["b"] != ports["a"]+1 {
		t.Errorf("expected sequential ports: a=%d, b=%d", ports["a"], ports["b"])
	}
	if ports["c"] != ports["b"]+1 {
		t.Errorf("expected sequential ports: b=%d, c=%d", ports["b"], ports["c"])
	}
}
