package domain

import "testing"

func TestAllocatePortOffset_Deterministic(t *testing.T) {
	offset1 := AllocatePortOffset("feat-auth")
	offset2 := AllocatePortOffset("feat-auth")
	if offset1 != offset2 {
		t.Errorf("expected deterministic offset, got %d and %d", offset1, offset2)
	}
}

func TestAllocatePortOffset_InRange(t *testing.T) {
	names := []string{
		"feat-auth", "feat-payments", "bugfix-login", "main",
		"release-v2", "hotfix-42", "test-env", "dev",
		"a", "very-long-environment-name-that-should-still-work",
	}
	for _, name := range names {
		offset := AllocatePortOffset(name)
		if offset < 1 || offset > maxOffset {
			t.Errorf("offset for '%s' is %d, expected [1, %d]", name, offset, maxOffset)
		}
	}
}

func TestAllocatePortOffset_DifferentNames(t *testing.T) {
	offset1 := AllocatePortOffset("feat-auth")
	offset2 := AllocatePortOffset("feat-payments")
	// Not guaranteed to be different for all pairs, but these two should differ
	// due to FNV-1a properties. This is a sanity check, not a guarantee.
	if offset1 == offset2 {
		t.Logf("warning: offsets for 'feat-auth' and 'feat-payments' collide at %d", offset1)
	}
}

func TestAllocatePorts(t *testing.T) {
	basePorts := map[string]int{
		"backend": 8000,
		"web":     3000,
		"redis":   6379,
	}

	ports := AllocatePorts("feat-auth", basePorts)
	offset := AllocatePortOffset("feat-auth")

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

func TestAllocatePorts_NeverCollidesWithBase(t *testing.T) {
	basePorts := map[string]int{
		"backend": 8000,
	}

	// Since offset is always >= 1, allocated port is always > base port
	names := []string{"a", "b", "c", "test", "prod", "staging"}
	for _, name := range names {
		ports := AllocatePorts(name, basePorts)
		if ports["backend"] <= 8000 {
			t.Errorf("allocated port %d for '%s' collides with base port 8000", ports["backend"], name)
		}
	}
}
