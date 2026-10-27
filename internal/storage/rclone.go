package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// RcloneService wraps `rclone` subprocess calls. Every command runs under a
// hard timeout so a hung remote (or a stalled rclone process) cannot wedge
// the planner tick loop or the engine probe path indefinitely — a stuck
// `rclone cat` used to block a whole channel's EPG walk, and with parallel
// channel processing (PLANNER_WORKERS) an unbounded hang would stall every
// worker that touches the bucket.
type RcloneService struct {
	verbose bool
}

func NewRcloneService(verbose bool) *RcloneService {
	return &RcloneService{verbose: verbose}
}

// rcloneEnv strips rclone's own RCLONE_VERBOSE env var (which conflicts with
// our SIMLIVE_RCLONE_VERBOSE; rclone expects RCLONE_VERBOSE to be an int count
// so "true"/"1" causes a startup crash before any HTTP request is made).
// All other RCLONE_* vars (config, logging, auth) are preserved.
func rcloneEnv() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !bytes.HasPrefix([]byte(e), []byte("RCLONE_VERBOSE=")) {
			out = append(out, e)
		}
	}
	return out
}

// catTimeout bounds `rclone cat` (manifest reads: per-stream m3u8 + the ads
// pool m3u8). 30s is generous for a small text file; an unreachable remote
// fails fast instead of hanging the worker.
const catTimeout = 10 * time.Second

// copyTimeout bounds `rclone copyto` (segment downloads for probing).
const copyTimeout = 10 * time.Second

// Cat reads an M3U8 manifest from S3/MinIO via `rclone cat`
func (r *RcloneService) Cat(remotePath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), catTimeout)
	defer cancel()

	args := []string{"cat"}
	if r.verbose {
		args = append(args, "-vv")
	}
	args = append(args, remotePath)

	cmd := exec.CommandContext(ctx, "rclone", args...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	cmd.Env = rcloneEnv()

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		return "", fmt.Errorf("rclone cat error: %v, stderr: %s, elapsed: %s", err, stderr.String(), elapsed)
	}
	return out.String(), nil
}

// CopyTo downloads a `.ts` file to the local buffer directory
func (r *RcloneService) CopyTo(parentCtx context.Context, srcRemotePath, localDestPath string) error {
	ctx, cancel := context.WithTimeout(parentCtx, copyTimeout)
	defer cancel()

	args := []string{"copyto"}
	if r.verbose {
		args = append(args, "-vv")
	}
	args = append(args, srcRemotePath, localDestPath)

	cmd := exec.CommandContext(ctx, "rclone", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = rcloneEnv()

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		return fmt.Errorf("rclone copyto error: %v, stderr: %s, elapsed: %s", err, stderr.String(), elapsed)
	}
	return nil
}