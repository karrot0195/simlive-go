package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	TMP = "/tmp/simlive-go/7261da4b-9c45-4619-bfb0-a6119dc7b6e6/360p/"
)

// SafeFileTracker keeps track of files processed across goroutines safely.
type SafeFileTracker struct {
	mu    sync.Mutex
	files map[string]bool
}

func NewSafeFileTracker() *SafeFileTracker {
	return &SafeFileTracker{
		files: make(map[string]bool),
	}
}

func (s *SafeFileTracker) Has(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[path]
}

func (s *SafeFileTracker) Add(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = true
}

func processFile(filePath string) bool {
	// Context timeout so ffprobe never freezes permanently
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "csv=print_section=0",
		filePath,
	)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Printf("⏳ Timeout reading %s (file might still be writing...)\n", filepath.Base(filePath))
		return false
	}

	if err != nil {
		fmt.Printf("⚠️ Could not read stream data for %s: %v\n", filepath.Base(filePath), err)
		return false
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var ptsStart, ptsEnd, lastDuration float64 = -1, -1, 0
	firstValid := true

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) > 0 {
			var pts float64
			if _, err := fmt.Sscanf(parts[0], "%f", &pts); err == nil {
				if firstValid {
					ptsStart = pts
					firstValid = false
				}
				ptsEnd = pts
			}
			// Parse duration of the last packet if available to accurately calculate total length
			if len(parts) > 1 {
				var dur float64
				if _, err := fmt.Sscanf(parts[1], "%f", &dur); err == nil {
					lastDuration = dur
				}
			}
		}
	}

	if firstValid {
		fmt.Printf("   └─ ⚠️ No valid PTS time found in packets.\n")
		return false
	}

	// Accurate segment duration includes the duration of the final packet
	duration := (ptsEnd - ptsStart) + lastDuration
	fmt.Printf("   └─ PTS Start: %.3fs | PTS End: %.3fs | Duration: %.3fs\n", ptsStart, ptsEnd, duration)
	return true
}

func scanAndProcess(folderPath string, tracker *SafeFileTracker) {
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return
	}

	// Sort entries chronologically by name
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		// Ignore directories
		if entry.IsDir() {
			continue
		}

		// Filter: Process ONLY MPEG-TS (.ts) files
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".ts") {
			continue
		}

		filePath := filepath.Join(folderPath, entry.Name())

		if tracker.Has(filePath) {
			continue
		}

		fmt.Printf("\n✨ [New TS File Detected] ➔ %s/%s\n", filepath.Base(folderPath), entry.Name())

		// Only mark as processed if ffprobe succeeds
		if success := processFile(filePath); success {
			tracker.Add(filePath)
		}
	}
}

func main() {
	if err := os.MkdirAll(TMP, 0755); err != nil {
		fmt.Printf("Error ensuring directory %s exists: %v\n", TMP, err)
		return
	}

	tracker := NewSafeFileTracker()
	activeMonitors := make(map[string]bool)
	var mu sync.Mutex

	fmt.Printf("👀 Verifier watcher started. Monitoring %s...\n", TMP)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Periodically scan base directory for existing and newly created subdirectories
		dirNames, err := os.ReadDir(TMP)
		if err != nil {
			fmt.Printf("Error reading base directory: %v\n", err)
			continue
		}

		// Also check files directly inside the base path (if media files are placed directly in TMP)
		scanAndProcess(TMP, tracker)

		for _, dirName := range dirNames {
			if !dirName.IsDir() {
				continue
			}

			subDirPath := filepath.Join(TMP, dirName.Name())

			mu.Lock()
			if activeMonitors[subDirPath] {
				mu.Unlock()
				continue
			}
			activeMonitors[subDirPath] = true
			mu.Unlock()

			// Launch dedicated watcher goroutine for new subfolder
			go func(path string) {
				subTicker := time.NewTicker(1 * time.Second)
				defer subTicker.Stop()

				for range subTicker.C {
					scanAndProcess(path, tracker)
				}
			}(subDirPath)
		}
	}
}
