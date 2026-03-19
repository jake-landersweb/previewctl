package s3

import (
	"context"
	"fmt"
)

// NoopDownloader returns an error when called. Used when no S3 config exists.
type NoopDownloader struct{}

func (NoopDownloader) Download(_ context.Context, bucket, key, _ string) error {
	return fmt.Errorf("S3 download not configured (attempted s3://%s/%s)", bucket, key)
}
