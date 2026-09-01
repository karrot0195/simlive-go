package storage

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// RcloneService wraps `rclone` subprocess calls. Every command runs under a
// hard timeout so a hung remote (or a stalled rclone process) cannot wedge
// the planner tick loop or the engine probe path indefinitely — a stuck
// `rclone cat` used to block a whole channel's EPG walk, and with parallel
// channel processing (PLANNER_WORKERS) an unbounded hang would stall every
// worker that touches the bucket.
type RcloneService struct{}

func NewRcloneService() *RcloneService {
	return &RcloneService{}
}

// catTimeout bounds `rclone cat` (manifest reads: per-stream m3u8 + the ads
// pool m3u8). 30s is generous for a small text file; an unreachable remote
// fails fast instead of hanging the worker.
const catTimeout = 30 * time.Second

// copyTimeout bounds `rclone copyto` (segment downloads for probing).
const copyTimeout = 60 * time.Second

// Cat reads an M3U8 manifest from S3/MinIO via `rclone cat`
func (r *RcloneService) Cat(remotePath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), catTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rclone", "cat", remotePath)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("rclone cat error: %v, stderr: %s", err, stderr.String())
	}
	return out.String(), nil
}

// CopyTo downloads a `.ts` file to the local buffer directory
func (r *RcloneService) CopyTo(srcRemotePath, localDestPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rclone", "copyto", srcRemotePath, localDestPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("rclone copyto error: %v, stderr: %s", err, stderr.String())
	}
	return nil
}
