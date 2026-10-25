package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"simlive/internal/verify"
)

const (
	BaseTmp    = "./tmp/simlive-go"
	DefaultRes = "360p"
	ProbeLimit = 5 * time.Second
	ScanEvery  = 1 * time.Second

	// US1 expectations
	ExpectedSCTE35PID       = 0x1F00
	ExpectedSCTE35StreamTyp = 0x86
	ExpectedRegistration    = "CUEI"
)

// ---------- tracker ----------

type SafeFileTracker struct {
	mu    sync.Mutex
	files map[string]bool
}

func NewSafeFileTracker() *SafeFileTracker {
	return &SafeFileTracker{files: make(map[string]bool)}
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

// ---------- ffprobe (duration) ----------

func processFile(filePath string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), verify.ProbeLimit)
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
		if len(parts) == 0 {
			continue
		}
		var pts float64
		if _, err := fmt.Sscanf(parts[0], "%f", &pts); err == nil {
			if firstValid {
				ptsStart = pts
				firstValid = false
			}
			ptsEnd = pts
		}
		if len(parts) > 1 {
			var dur float64
			if _, err := fmt.Sscanf(parts[1], "%f", &dur); err == nil {
				lastDuration = dur
			}
		}
	}

	if firstValid {
		fmt.Printf("   └─ ⚠️ No valid PTS time found in packets.\n")
		return false
	}

	duration := (ptsEnd - ptsStart) + lastDuration
	fmt.Printf("   └─ PTS Start: %.3fs | PTS End: %.3fs | Duration: %.3fs\n", ptsStart, ptsEnd, duration)
	return true
}

// ---------- per-file verification ----------

type VerifyOptions struct {
	ShowPmt    bool
	ShowTspPsi bool
	TspTimeout time.Duration
}

func verifySegment(filePath string, baseline *verify.PmtBaseline, opts VerifyOptions) bool {
	name := filepath.Base(filePath)
	fmt.Printf("\n✨ [New TS File Detected] ➔ %s/%s\n", filepath.Base(filepath.Dir(filePath)), name)

	// 1) Duration.
	if !processFile(filePath) {
		return false
	}

	if !opts.ShowPmt {
		return true
	}

	// 2) PMT inspection.
	pmt, err := verify.InspectPmt(filePath)
	if err != nil {
		fmt.Printf("   └─ ⚠️ PMT inspect failed: %v\n", err)
		return false
	}

	fmt.Printf("   └─ 🧬 PMT fingerprint: %s (%d stream(s))\n", pmt.Fingerprint, len(pmt.Streams))
	for _, s := range pmt.Streams {
		tag := ""
		if s.IsSCTE35 {
			tag = "  ← SCTE-35"
		}
		fmt.Printf("        • PID 0x%04X  stream_type 0x%02X  codec=%-8s%s\n",
			s.PID, s.StreamType, s.CodecName, tag)
	}

	scte := pmt.FindSCTE35()
	if scte == nil {
		fmt.Printf("   └─ ❌ SCTE-35 stream NOT FOUND in PMT\n")
	} else {
		pidOk := scte.PID == verify.ExpectedSCTE35PID || scte.PID == 0
		if pidOk {
			fmt.Printf("   └─ ✅ SCTE-35 declared: PID 0x%04X, stream_type 0x%02X\n",
				scte.PID, scte.StreamType)
		} else {
			fmt.Printf("   └─ ⚠️ SCTE-35 declared: PID 0x%04X (expected 0x%04X), stream_type 0x%02X\n",
				scte.PID, verify.ExpectedSCTE35PID, scte.StreamType)
		}
		if scte.StreamType != verify.ExpectedSCTE35StreamTyp {
			fmt.Printf("      ⚠️ Expected stream_type 0x%02X, got 0x%02X\n",
				verify.ExpectedSCTE35StreamTyp, scte.StreamType)
		}
	}

	// 3) Cross-segment consistency.
	base, consistent, first := baseline.Check(pmt.Fingerprint)
	if first {
		fmt.Printf("   └─ 📌 PMT baseline set (first segment)\n")
	} else if !consistent {
		fmt.Printf("   └─ ❌ PMT DIFFERS from baseline %s — PMT content is NOT identical across segments!\n", base)
	} else {
		fmt.Printf("   └─ ✅ PMT matches baseline %s\n", base)
	}

	// 4) Optional raw PSI dump (tsduck).
	if opts.ShowTspPsi {
		raw, err := verify.RawPsi(filePath, opts.TspTimeout)
		if err != nil {
			fmt.Printf("   └─ (tsduck psi skipped: %v)\n", err)
		} else {
			pmtLine, scteLine, cuei, regCount := verify.SummarizeRawPsi(raw)
			if pmtLine != "" {
				fmt.Printf("   └─ 🧾 %s\n", pmtLine)
			}
			if scteLine != "" {
				fmt.Printf("        %s\n", scteLine)
			}
			if cuei {
				fmt.Printf("   └─ ✅ Registration descriptor %q present (count: %d)\n", verify.ExpectedRegistration, regCount)
				if regCount > 1 {
					fmt.Printf("   └─ ⚠️ Duplicate registration descriptors (%d) — tsp --add-registration is not idempotent\n", regCount)
				}
			} else {
				fmt.Printf("   └─ ⚠️ Registration descriptor %q NOT found in PSI\n", verify.ExpectedRegistration)
			}
		}
	}

	return true
}

// ---------- scanner ----------

func scanAndProcess(folderPath string, tracker *SafeFileTracker, baseline *verify.PmtBaseline, opts VerifyOptions) {
	entries, err := os.ReadDir(folderPath)
	if err != nil {
		return
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".ts") {
			continue
		}

		filePath := filepath.Join(folderPath, entry.Name())
		if tracker.Has(filePath) {
			continue
		}

		if verifySegment(filePath, baseline, opts) {
			tracker.Add(filePath)
		}
	}
}

// ---------- interactive prompt ----------

func listExistingChannels() []string {
	entries, err := os.ReadDir(BaseTmp)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func chooseChannel(reader *bufio.Reader) string {
	existing := listExistingChannels()

	if len(existing) > 0 {
		fmt.Println("Existing channels:")
		for i, ch := range existing {
			fmt.Printf("  [%d] %s\n", i+1, ch)
		}
		fmt.Printf("  [n] Enter a new channel ID\n")
	}

	for {
		var hint string
		if len(existing) > 0 {
			hint = "Select [1-n] or type a channel ID"
		} else {
			hint = "Enter channel ID"
		}
		fmt.Printf("%s: ", hint)
		line, err := reader.ReadString('\n')
		if err != nil {
			return ""
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if idx := atoiSafe(line); idx > 0 && idx <= len(existing) {
			return existing[idx-1]
		}
		if strings.EqualFold(line, "n") {
			fmt.Print("Enter new channel ID: ")
			id, err := reader.ReadString('\n')
			if err != nil {
				return ""
			}
			id = strings.TrimSpace(id)
			if id != "" {
				return id
			}
			continue
		}
		return line
	}
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// ---------- main ----------

func main() {
	flagChannel := flag.String("channel", "", "channel ID (skips interactive prompt)")
	flagRes := flag.String("res", DefaultRes, "resolution (default: 360p)")
	flagPmt := flag.Bool("pmt", true, "inspect PMT (SCTE-35 presence, fingerprint, consistency)")
	flagTsp := flag.Bool("tsp", false, "also run `tsp -P psi` (tsduck) to show raw PMT + CUEI registration")
	flag.Parse()

	isTTY := term.IsTerminal(int(os.Stdin.Fd()))

	channelID := *flagChannel
	resolution := *flagRes

	if channelID == "" && isTTY {
		reader := bufio.NewReader(os.Stdin)
		fmt.Println("📺 SimLive Verifier")
		fmt.Println(strings.Repeat("-", 40))
		channelID = chooseChannel(reader)
		if channelID == "" {
			fmt.Println("⚠️  No channel selected. Aborting.")
			return
		}
		if resolution == DefaultRes {
			fmt.Printf("Enter resolution [%s]: ", DefaultRes)
			if line, err := reader.ReadString('\n'); err == nil {
				if v := strings.TrimSpace(line); v != "" {
					resolution = v
				}
			}
		}
	} else if channelID == "" {
		fmt.Println("❌ Non-interactive shell and no -channel flag supplied.")
		fmt.Println("   Usage: verifier -channel <id> [-res 360p] [-pmt=true] [-tsp]")
		os.Exit(1)
	}

	TMP := filepath.Join(BaseTmp, channelID, resolution)
	if err := os.MkdirAll(TMP, 0755); err != nil {
		fmt.Printf("Error ensuring directory %s exists: %v\n", TMP, err)
		return
	}

	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("📂 Monitoring: %s\n", TMP)
	fmt.Printf("📡 Channel:    %s\n", channelID)
	fmt.Printf("🎞️  Resolution: %s\n", resolution)
	fmt.Printf("🔬 PMT checks: %v", *flagPmt)
	if *flagPmt && *flagTsp {
		fmt.Printf(" (+ tsduck psi dump)")
	}
	fmt.Println()
	fmt.Println(strings.Repeat("-", 40))

	tracker := NewSafeFileTracker()
	baseline := &verify.PmtBaseline{}
	activeMonitors := make(map[string]bool)
	var mu sync.Mutex

	opts := VerifyOptions{
		ShowPmt:    *flagPmt,
		ShowTspPsi: *flagTsp,
		TspTimeout: 5 * time.Second,
	}

	fmt.Printf("👀 Verifier watcher started. Monitoring %s...\n", TMP)

	ticker := time.NewTicker(ScanEvery)
	defer ticker.Stop()

	for range ticker.C {
		dirNames, err := os.ReadDir(TMP)
		if err != nil {
			fmt.Printf("Error reading base directory: %v\n", err)
			continue
		}

		scanAndProcess(TMP, tracker, baseline, opts)

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

			go func(path string) {
				subTicker := time.NewTicker(ScanEvery)
				defer subTicker.Stop()
				for range subTicker.C {
					scanAndProcess(path, tracker, baseline, opts)
				}
			}(subDirPath)
		}
	}
}
