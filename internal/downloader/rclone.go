package downloader

import (
	"context"
	"fmt"

	"simlive/internal/storage"
)

// RcloneDownload obtains data using rclone.
type RcloneDownload struct {
	svc *storage.RcloneService
}

// NewRclone creates a new RcloneDownload.
func NewRclone(svc *storage.RcloneService) *RcloneDownload {
	if svc == nil {
		svc = storage.NewRcloneService()
	}
	return &RcloneDownload{svc: svc}
}

// Fetch reads data from the given remote/source path via rclone.
func (r *RcloneDownload) Fetch(ctx context.Context, source string) ([]byte, error) {
	if r == nil || r.svc == nil {
		return nil, fmt.Errorf("rclone downloader not initialized")
	}
	out, err := r.svc.Cat(ctx, source)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}
