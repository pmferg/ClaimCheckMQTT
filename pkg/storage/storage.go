package storage

import (
	"context"
	"io"
)

// PayloadStore abstracts payload persistence so the gateway and clients
// are not coupled directly to MinIO.
type PayloadStore interface {
	// Put stores the payload readable from r under the given MPID.
	Put(ctx context.Context, mpid string, r io.Reader, size int64, contentType string) error

	// Get retrieves the payload for the given MPID.
	Get(ctx context.Context, mpid string) (io.ReadCloser, error)

	// Delete removes the payload for the given MPID.
	Delete(ctx context.Context, mpid string) error
}
