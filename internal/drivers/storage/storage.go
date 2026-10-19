// Package storage opens the deployment's object storage bucket through
// gocloud.dev/blob: S3 and S3-compatible (MinIO, ODF/NooBaa), Azure Blob,
// GCS, or a local folder.
//
//	s3://kitmaker?endpoint=http://minio:9000&use_path_style=true&region=us-east-1
//	azblob://kitmaker
//	gs://kitmaker
//	file:///var/lib/kitmaker/blobs?create_dir=true
//	mem://
package storage

import (
	"context"
	"fmt"

	"gocloud.dev/blob"
	_ "gocloud.dev/blob/azureblob"
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/memblob"
	_ "gocloud.dev/blob/s3blob"
)

// Open opens the bucket at url.
func Open(ctx context.Context, url string) (*blob.Bucket, error) {
	b, err := blob.OpenBucket(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open bucket: %w", err)
	}
	return b, nil
}
