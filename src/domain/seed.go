package domain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SeedKind describes the type of a seed step material.
type SeedKind string

const (
	SeedNone    SeedKind = "none"
	SeedSQL     SeedKind = "sql"
	SeedDump    SeedKind = "dump"
	SeedCommand SeedKind = "run"
)

// SeedMaterial is a resolved, ready-to-execute seed step.
type SeedMaterial struct {
	Kind     SeedKind
	SQLPath  string // absolute path, set when Kind == SeedSQL
	DumpPath string // absolute path (decompressed), set when Kind == SeedDump
	Command  string // shell command, set when Kind == SeedCommand
}

// SeedResolver resolves SeedStep configs into ready-to-apply SeedMaterials.
// It handles path resolution, S3 downloads, and decompression.
type SeedResolver struct {
	downloader S3Downloader
}

// NewSeedResolver creates a SeedResolver with the given S3 downloader.
func NewSeedResolver(downloader S3Downloader) *SeedResolver {
	return &SeedResolver{downloader: downloader}
}

// Resolve processes all seed steps into resolved materials.
func (r *SeedResolver) Resolve(ctx context.Context, steps []SeedStep, projectRoot string) ([]*SeedMaterial, error) {
	if len(steps) == 0 {
		return nil, nil
	}

	materials := make([]*SeedMaterial, 0, len(steps))
	for i, step := range steps {
		mat, err := r.resolveStep(ctx, step, projectRoot)
		if err != nil {
			return nil, fmt.Errorf("seed step %d: %w", i+1, err)
		}
		materials = append(materials, mat)
	}
	return materials, nil
}

func (r *SeedResolver) resolveStep(ctx context.Context, step SeedStep, projectRoot string) (*SeedMaterial, error) {
	switch {
	case step.SQL != "":
		path := step.SQL
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectRoot, path)
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("sql file not found: %s", path)
		}
		return &SeedMaterial{Kind: SeedSQL, SQLPath: path}, nil

	case step.Dump != "":
		path := step.Dump
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectRoot, path)
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("dump file not found: %s", path)
		}
		resolved, err := decompress(path)
		if err != nil {
			return nil, fmt.Errorf("decompressing dump: %w", err)
		}
		return &SeedMaterial{Kind: SeedDump, DumpPath: resolved}, nil

	case step.S3 != nil:
		tmpDir, err := os.MkdirTemp("", "previewctl-s3-*")
		if err != nil {
			return nil, fmt.Errorf("creating temp dir: %w", err)
		}
		destPath := filepath.Join(tmpDir, filepath.Base(step.S3.Key))
		if err := r.downloader.Download(ctx, step.S3.Bucket, step.S3.Key, destPath); err != nil {
			return nil, fmt.Errorf("downloading from s3://%s/%s: %w", step.S3.Bucket, step.S3.Key, err)
		}
		resolved, err := decompress(destPath)
		if err != nil {
			return nil, fmt.Errorf("decompressing s3 download: %w", err)
		}
		return &SeedMaterial{Kind: SeedDump, DumpPath: resolved}, nil

	case step.Run != "":
		return &SeedMaterial{Kind: SeedCommand, Command: step.Run}, nil

	default:
		return nil, fmt.Errorf("empty seed step")
	}
}

// decompress handles .gz and .tar.gz/.tgz files.
// Returns the path to the usable file (original path if not compressed).
func decompress(path string) (string, error) {
	lower := strings.ToLower(path)

	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return decompressTarGz(path)
	case strings.HasSuffix(lower, ".gz"):
		return decompressGz(path)
	default:
		return path, nil
	}
}

// decompressGz gunzips a .gz file, writing the result next to it with .gz stripped.
func decompressGz(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("opening gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	outPath := strings.TrimSuffix(path, filepath.Ext(path))
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, gz); err != nil {
		return "", fmt.Errorf("decompressing gzip: %w", err)
	}

	return outPath, nil
}

// decompressTarGz extracts a .tar.gz and returns the path to the first regular file found.
func decompressTarGz(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("opening gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	outDir := filepath.Dir(path)
	var firstFile string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading tar: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		outPath := filepath.Join(outDir, filepath.Base(hdr.Name))
		out, err := os.Create(outPath)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return "", fmt.Errorf("extracting %s: %w", hdr.Name, err)
		}
		_ = out.Close()

		if firstFile == "" {
			firstFile = outPath
		}
	}

	if firstFile == "" {
		return "", fmt.Errorf("tar archive is empty")
	}
	return firstFile, nil
}
