package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalComputeAccess_WriteFile(t *testing.T) {
	root := t.TempDir()
	ca := NewLocalComputeAccess(root)
	ctx := context.Background()

	err := ca.WriteFile(ctx, "subdir/test.txt", []byte("hello"), 0o644)
	if err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "subdir", "test.txt"))
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got '%s'", string(data))
	}
}

func TestLocalComputeAccess_ReadFile(t *testing.T) {
	root := t.TempDir()
	ca := NewLocalComputeAccess(root)
	ctx := context.Background()

	expected := "test content"
	if err := os.WriteFile(filepath.Join(root, "test.txt"), []byte(expected), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := ca.ReadFile(ctx, "test.txt")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != expected {
		t.Errorf("expected '%s', got '%s'", expected, string(data))
	}
}

func TestLocalComputeAccess_Exec(t *testing.T) {
	root := t.TempDir()
	ca := NewLocalComputeAccess(root)
	ctx := context.Background()

	stdout, err := ca.Exec(ctx, "echo hello", os.Environ())
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if strings.TrimSpace(stdout) != "hello" {
		t.Errorf("expected 'hello', got '%s'", strings.TrimSpace(stdout))
	}
}

func TestLocalComputeAccess_Root(t *testing.T) {
	root := t.TempDir()
	ca := NewLocalComputeAccess(root)
	if ca.Root() != root {
		t.Errorf("expected root '%s', got '%s'", root, ca.Root())
	}
}

func TestLocalComputeAccess_ExecWorkdir(t *testing.T) {
	root := t.TempDir()
	ca := NewLocalComputeAccess(root)
	ctx := context.Background()

	// Write a marker file to verify we're in the right directory
	markerPath := filepath.Join(root, "marker.txt")
	if err := os.WriteFile(markerPath, []byte("found"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, err := ca.Exec(ctx, "cat marker.txt", os.Environ())
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if strings.TrimSpace(stdout) != "found" {
		t.Errorf("expected 'found', got '%s'", strings.TrimSpace(stdout))
	}
}
