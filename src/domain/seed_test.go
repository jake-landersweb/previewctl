package domain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSeedResolver_SQL(t *testing.T) {
	dir := t.TempDir()
	sqlFile := filepath.Join(dir, "seed.sql")
	_ = os.WriteFile(sqlFile, []byte("CREATE TABLE t(id int);"), 0o644)

	resolver := NewSeedResolver()
	materials, err := resolver.Resolve(context.Background(), []SeedStep{
		{SQL: sqlFile},
	}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(materials) != 1 {
		t.Fatalf("expected 1 material, got %d", len(materials))
	}
	if materials[0].Kind != SeedSQL {
		t.Errorf("expected SeedSQL, got %s", materials[0].Kind)
	}
	if materials[0].SQLPath != sqlFile {
		t.Errorf("expected path %s, got %s", sqlFile, materials[0].SQLPath)
	}
}

func TestSeedResolver_SQL_Relative(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "schema.sql"), []byte("SELECT 1;"), 0o644)

	resolver := NewSeedResolver()
	materials, err := resolver.Resolve(context.Background(), []SeedStep{
		{SQL: "schema.sql"},
	}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if materials[0].SQLPath != filepath.Join(dir, "schema.sql") {
		t.Errorf("expected absolute path, got %s", materials[0].SQLPath)
	}
}

func TestSeedResolver_SQL_NotFound(t *testing.T) {
	resolver := NewSeedResolver()
	_, err := resolver.Resolve(context.Background(), []SeedStep{
		{SQL: "/nonexistent/file.sql"},
	}, "/tmp")
	if err == nil {
		t.Fatal("expected error for missing sql file")
	}
}

func TestSeedResolver_Dump(t *testing.T) {
	dir := t.TempDir()
	dumpFile := filepath.Join(dir, "data.dump")
	_ = os.WriteFile(dumpFile, []byte("fake dump"), 0o644)

	resolver := NewSeedResolver()
	materials, err := resolver.Resolve(context.Background(), []SeedStep{
		{Dump: dumpFile},
	}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if materials[0].Kind != SeedDump {
		t.Errorf("expected SeedDump, got %s", materials[0].Kind)
	}
	if materials[0].DumpPath != dumpFile {
		t.Errorf("expected path %s, got %s", dumpFile, materials[0].DumpPath)
	}
}

func TestSeedResolver_DumpGz(t *testing.T) {
	dir := t.TempDir()
	gzPath := filepath.Join(dir, "data.dump.gz")

	// Create a gzipped file
	f, _ := os.Create(gzPath)
	gz := gzip.NewWriter(f)
	_, _ = gz.Write([]byte("fake dump data"))
	_ = gz.Close()
	_ = f.Close()

	resolver := NewSeedResolver()
	materials, err := resolver.Resolve(context.Background(), []SeedStep{
		{Dump: gzPath},
	}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should decompress — path should NOT end in .gz
	if filepath.Ext(materials[0].DumpPath) == ".gz" {
		t.Errorf("expected decompressed path, got %s", materials[0].DumpPath)
	}

	// Verify content
	data, err := os.ReadFile(materials[0].DumpPath)
	if err != nil {
		t.Fatalf("reading decompressed file: %v", err)
	}
	if string(data) != "fake dump data" {
		t.Errorf("unexpected content: %s", string(data))
	}
}

func TestSeedResolver_DumpTarGz(t *testing.T) {
	dir := t.TempDir()
	tarGzPath := filepath.Join(dir, "data.tar.gz")

	// Create a tar.gz with a single file
	f, _ := os.Create(tarGzPath)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	content := []byte("fake dump from tar")
	_ = tw.WriteHeader(&tar.Header{
		Name: "dump.sql",
		Size: int64(len(content)),
		Mode: 0o644,
	})
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	resolver := NewSeedResolver()
	materials, err := resolver.Resolve(context.Background(), []SeedStep{
		{Dump: tarGzPath},
	}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(materials[0].DumpPath)
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if string(data) != "fake dump from tar" {
		t.Errorf("unexpected content: %s", string(data))
	}
}

func TestSeedResolver_Run(t *testing.T) {
	resolver := NewSeedResolver()
	materials, err := resolver.Resolve(context.Background(), []SeedStep{
		{Run: "echo hello"},
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if materials[0].Kind != SeedCommand {
		t.Errorf("expected SeedCommand, got %s", materials[0].Kind)
	}
	if materials[0].Command != "echo hello" {
		t.Errorf("expected 'echo hello', got '%s'", materials[0].Command)
	}
}

func TestSeedResolver_Pipeline(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "schema.sql"), []byte("CREATE TABLE t(id int);"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "data.dump"), []byte("fake dump"), 0o644)

	resolver := NewSeedResolver()
	materials, err := resolver.Resolve(context.Background(), []SeedStep{
		{SQL: "schema.sql"},
		{Dump: "data.dump"},
		{Run: "npm run migrate"},
	}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(materials) != 3 {
		t.Fatalf("expected 3 materials, got %d", len(materials))
	}
	if materials[0].Kind != SeedSQL {
		t.Errorf("step 0: expected SeedSQL, got %s", materials[0].Kind)
	}
	if materials[1].Kind != SeedDump {
		t.Errorf("step 1: expected SeedDump, got %s", materials[1].Kind)
	}
	if materials[2].Kind != SeedCommand {
		t.Errorf("step 2: expected SeedCommand, got %s", materials[2].Kind)
	}
}

func TestSeedResolver_Empty(t *testing.T) {
	resolver := NewSeedResolver()
	materials, err := resolver.Resolve(context.Background(), nil, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if materials != nil {
		t.Errorf("expected nil materials for empty steps, got %v", materials)
	}
}

// noopS3Downloader is defined in manager_test.go (same package)
