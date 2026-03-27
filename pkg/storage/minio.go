package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOConfig holds connection parameters for a MinIO instance.
type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type minioStore struct {
	client *minio.Client
	bucket string
}

// NewMinIO creates a PayloadStore backed by MinIO.
// It ensures the target bucket exists on startup.
func NewMinIO(ctx context.Context, cfg MinIOConfig) (PayloadStore, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket %q: %w", cfg.Bucket, err)
		}
		slog.Info("created minio bucket", "bucket", cfg.Bucket)
	}

	return &minioStore{client: client, bucket: cfg.Bucket}, nil
}

func (s *minioStore) Put(ctx context.Context, mpid string, r io.Reader, size int64, contentType string) error {
	opts := minio.PutObjectOptions{
		ContentType: contentType,
		Expires:     time.Now().Add(24 * time.Hour),
	}
	_, err := s.client.PutObject(ctx, s.bucket, mpid, r, size, opts)
	if err != nil {
		return fmt.Errorf("put %s: %w", mpid, err)
	}
	return nil
}

func (s *minioStore) Get(ctx context.Context, mpid string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, mpid, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", mpid, err)
	}
	// Trigger a stat so we fail fast on missing keys.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, fmt.Errorf("stat %s: %w", mpid, err)
	}
	return obj, nil
}

func (s *minioStore) Delete(ctx context.Context, mpid string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, mpid, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete %s: %w", mpid, err)
	}
	return nil
}
