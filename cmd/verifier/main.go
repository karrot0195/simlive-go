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
	TMP = "./outputs"
)

// SafeFileTracker keeps track of files we've already seen across goroutines safely
type SafeFileTracker struct {
	mu    sync.Mutex
	files map[string]bool
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

func processAnalytics(folderPath string, tracker *SafeFileTracker) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		entries, err := os.ReadDir(folderPath)
		if err != nil {
			// Avoid spamming if directory is temporarily missing or locked
			continue
		}

		// Sort entries by name
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			filePath := filepath.Join(folderPath, entry.Name())

			// Check if we already processed this file using our Map tracker
			if tracker.Has(filePath) {
				continue
			}

			// Mark as seen immediately so it won't trigger again
			tracker.Add(filePath)

			fmt.Printf("\n✨ [New File Detected] ➔ %s/%s\n", filepath.Base(folderPath), entry.Name())

			// Use a context timeout so ffprobe never freezes permanently
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cmd := exec.CommandContext(ctx, "ffprobe",
				"-v", "error",
				"-show_entries", "packet=pts_time,dts_time",
				"-of", "csv=print_section=0",
				filePath,
			)

			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = os.Stderr

			err = cmd.Run()
			cancel()

			if ctx.Err() == context.DeadlineExceeded {
				fmt.Printf("⏳ Timeout reading %s (file might still be writing...)\n", entry.Name())
				continue
			}

			if err != nil {
				fmt.Printf("⚠️ Could not read stream data for %s: %v\n", entry.Name(), err)
				continue
			}

			// Parse PTS Start, End, and calculate Duration from CSV output
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			var ptsStart, ptsEnd float64 = -1, -1
			firstValid := true

			for _, line := range lines {
				if line == "" {
					continue
				}
				parts := strings.Split(line, ",")
				if len(parts) > 0 {
					// parts[0] is pts_time
					var pts float64
					if _, err := fmt.Sscanf(parts[0], "%f", &pts); err == nil {
						if firstValid {
							ptsStart = pts
							firstValid = false
						}
						ptsEnd = pts // continuously update to capture the last packet's PTS
					}
				}
			}

			if firstValid {
				fmt.Printf("   └─ ⚠️ No valid PTS time found in packets.\n")
			} else {
				duration := ptsEnd - ptsStart
				fmt.Printf("   └─ PTS Start: %.3fs | PTS End: %.3fs | Duration: %.3fs\n", ptsStart, ptsEnd, duration)
			}
		}
	}
}

func main() {
	if _, err := os.Stat(TMP); os.IsNotExist(err) {
		fmt.Printf("📁 Directory %s does not exist yet. Waiting for outputs...\n", TMP)
		if err := os.MkdirAll(TMP, 0755); err != nil {
			fmt.Printf("Error creating directory %s: %v\n", TMP, err)
			return
		}
	}

	// Initialize our thread-safe file tracking map
	tracker := &SafeFileTracker{
		files: make(map[string]bool),
	}

	fmt.Println("👀 Verifier watcher started. Monitoring ./outputs/ directories...")

	// Scan initial directories
	dirNames, err := os.ReadDir(TMP)
	if err != nil {
		fmt.Printf("Error reading base directory: %v\n", err)
		return
	}

	for _, dirName := range dirNames {
		if !dirName.IsDir() {
			continue
		}

		subDirPath := filepath.Join(TMP, dirName.Name())
		go processAnalytics(subDirPath, tracker)
	}

	// Keep the application running gracefully
	select {}
}
