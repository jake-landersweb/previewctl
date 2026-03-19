package s3

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Downloader implements domain.S3Downloader using the AWS SDK.
type Downloader struct{}

// NewDownloader creates a new S3 downloader.
func NewDownloader() *Downloader {
	return &Downloader{}
}

func (d *Downloader) Download(ctx context.Context, bucket, key, destPath string) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("getting s3://%s/%s: %w", bucket, key, err)
	}
	defer func() { _ = resp.Body.Close() }()

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", destPath, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.ReadFrom(resp.Body); err != nil {
		return fmt.Errorf("writing %s: %w", destPath, err)
	}

	return nil
}
