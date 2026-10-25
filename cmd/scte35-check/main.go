package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ExpectedSCTE35PID       = 0x1F00
	ExpectedSCTE35StreamTyp = 0x86
	ExpectedRegistration    = "CUEI"
)

// ---------- PMT consistency tracker ----------

type PmtBaseline struct {
	mu          sync.Mutex
	fingerprint string
	set         bool
}

func (p *PmtBaseline) Check(fp string) (baseline string, consistent bool, first bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.set {
		p.fingerprint = fp
		p.set = true
		return fp, true, true
	}
	return p.fingerprint, fp == p.fingerprint, false
}

// ---------- TSDuck runners ----------

func runTspSpliceMonitor(segURL string) (string, error) {
	cmdStr := fmt.Sprintf(
		"tsp -I http %q -P tables --no-duplicate -P splicemonitor -O drop",
		segURL,
	)
	cmd := exec.Command("sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("tsp execution failed: %w, stderr/stdout: %s", err, string(out))
	}
	return string(out), nil
}

func runTspPsi(segURL string) (string, error) {
	cmdStr := fmt.Sprintf(
		"tsp -I http %q -P psi -O drop 2>/dev/null",
		segURL,
	)
	cmd := exec.Command("sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tsp psi failed: %w", err)
	}
	return string(out), nil
}

// ---------- PMT parsing ----------

func parsePmtInfo(psiOutput string) (hasSCTE35 bool, pid int, streamType int, hasCUEI bool, version int) {
	hasSCTE35 = false
	pid = -1
	streamType = -1
	hasCUEI = false
	version = -1

	for _, line := range strings.Split(psiOutput, "\n") {
		l := strings.TrimSpace(line)

		// Extract version_number
		if strings.HasPrefix(l, "* PMT") {
			re := regexp.MustCompile(`Version:\s*(\d+)`)
			if matches := re.FindStringSubmatch(l); len(matches) > 1 {
				if v, err := strconv.Atoi(matches[1]); err == nil {
					version = v
				}
			}
		}

		// Check for CUEI registration descriptor
		if strings.Contains(l, ExpectedRegistration) {
			hasCUEI = true
		}

		// Check for SCTE-35 elementary stream (tsduck psi output uses
		// "Elementary stream: type 0x86 (SCTE 35 Splice Info), PID: 0x1F00 (...)").
		isSCTELine := strings.Contains(l, "stream_type: 0x86") ||
			(strings.Contains(l, "Elementary stream") && (strings.Contains(l, "SCTE") || strings.Contains(l, "0x86")))
		if isSCTELine {
			hasSCTE35 = true
			pidRe := regexp.MustCompile(`PID[:\s]+(0x[0-9a-fA-F]+|\d+)`)
			if matches := pidRe.FindStringSubmatch(l); len(matches) > 1 {
				v := matches[1]
				if strings.HasPrefix(v, "0x") {
					if p, err := strconv.ParseInt(v, 0, 64); err == nil {
						pid = int(p)
					}
				} else {
					if p, err := strconv.Atoi(v); err == nil {
						pid = p
					}
				}
			}
			typeRe := regexp.MustCompile(`type[:\s]+(0x[0-9a-fA-F]+|\d+)`)
			if matches := typeRe.FindStringSubmatch(l); len(matches) > 1 {
				v := matches[1]
				if p, err := strconv.ParseInt(v, 0, 64); err == nil {
					streamType = int(p)
				}
			}
		}
	}
	return
}

// ---------- PMT fingerprint ----------

func computePmtFingerprint(psiOutput string) string {
	var sb strings.Builder
	for _, line := range strings.Split(psiOutput, "\n") {
		l := strings.TrimSpace(line)
		// Only include elementary stream lines and registration descriptor lines
		if strings.Contains(l, "stream_type") || strings.Contains(l, "Registration") || strings.Contains(l, "Format identifier") {
			fmt.Fprintf(&sb, "%s;", l)
		}
	}
	return sb.String()
}

// ---------- verifier state ----------

type verifier struct {
	seenSegments map[string]string
	pmtBaseline  *PmtBaseline
}

func newVerifier() *verifier {
	return &verifier{
		seenSegments: map[string]string{},
		pmtBaseline:  &PmtBaseline{},
	}
}

func (v *verifier) fetchPlaylist(url string) {
	parts := strings.Split(url, "/")
	dirUrlStr := strings.Join(parts[:len(parts)-1], "/")

	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("http.Get failed:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("io.ReadAll failed:", err)
		return
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))

	isPreroll := false
	isCueOut := false
	isAds := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Update SCTE-35 state machine
		if strings.Contains(line, "EXT-X-SCTE35:CUE") {
			if strings.Contains(line, "TYPE=48") {
				isPreroll = true
			}
			if strings.Contains(line, "TYPE=52") {
				isCueOut = true
				isAds = true
			}
			if strings.Contains(line, "TYPE=53") {
				isCueOut = false
				isAds = false
			}
			continue
		}

		// Skip non-segment lines
		if strings.HasPrefix(line, "#") || !strings.HasSuffix(line, ".ts") {
			continue
		}

		// Skip segments already processed
		if _, ok := v.seenSegments[line]; ok {
			isPreroll = false
			isCueOut = false
			continue
		}

		segURL := dirUrlStr + "/" + line
		fmt.Printf("\n📡 Segment: %s\n", segURL)
		fmt.Println(strings.Repeat("─", 60))

		// 1) Run splicemonitor
		spliceOutput, spliceErr := runTspSpliceMonitor(segURL)
		v.seenSegments[line] = spliceOutput

		if spliceErr != nil {
			fmt.Printf("❌ tsp splicemonitor failed: %v\n", spliceErr)
		} else {
			// Determine label type based on current state
			var label string
			if isPreroll {
				label = "Preroll"
			} else if isCueOut {
				label = "CueOut"
			} else if isAds {
				label = "Ads"
			} else {
				label = "VOD"
			}
			fmt.Printf("[%s] SCTE-35 splice info:\n", label)
			fmt.Println(spliceOutput)
		}

		// 2) Run PSI inspection for PMT
		psiOutput, psiErr := runTspPsi(segURL)
		if psiErr != nil {
			fmt.Printf("⚠️  tsp psi failed: %v\n", psiErr)
		} else {
			hasSCTE35, pid, streamType, hasCUEI, version := parsePmtInfo(psiOutput)

			fmt.Printf("📋 PMT Info:\n")
			if version >= 0 {
				fmt.Printf("   Version: %d\n", version)
			}
			if hasSCTE35 {
				pidStr := "?"
				if pid >= 0 {
					pidStr = fmt.Sprintf("0x%04X", pid)
				}
				fmt.Printf("   ✅ SCTE-35 stream: PID %s, stream_type 0x%02X\n", pidStr, streamType)
				if pid >= 0 && pid != ExpectedSCTE35PID {
					fmt.Printf("   ⚠️  Expected PID 0x%04X, got %s\n", ExpectedSCTE35PID, pidStr)
				}
			} else {
				fmt.Printf("   ❌ SCTE-35 stream NOT FOUND in PMT\n")
			}

			if hasCUEI {
				fmt.Printf("   ✅ Registration descriptor %q present\n", ExpectedRegistration)
			} else {
				fmt.Printf("   ⚠️  Registration descriptor %q NOT found\n", ExpectedRegistration)
			}

			// PMT consistency check
			fp := computePmtFingerprint(psiOutput)
			if fp != "" {
				_, consistent, first := v.pmtBaseline.Check(fp)
				if first {
					fmt.Printf("   📌 PMT baseline set\n")
				} else if !consistent {
					fmt.Printf("   ❌ PMT DIFFERS from baseline — content changed!\n")
				} else {
					fmt.Printf("   ✅ PMT matches baseline\n")
				}
			}
		}

		fmt.Println(strings.Repeat("─", 60))

		// Reset non-sticky per-segment flags
		isPreroll = false
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("scanner error:", err)
	}
}

func main() {
	fmt.Println("📺 SCTE-35 PMT Verifier (HTTP)")
	fmt.Println(strings.Repeat("=", 40))

	api := flag.String("api", "http://localhost:8080", "API base URL")
	channel := flag.String("channel", "f1533ebe-dc74-4e85-a34e-a9bc4eedee78", "channel ID")
	res := flag.String("res", "360p", "resolution (360p/720p/1080p)")
	flag.Parse()

	v := newVerifier()

	url := fmt.Sprintf("%s/live/%s/%s/playlist.m3u8", *api, *channel, *res)
	fmt.Printf("📡 Monitoring: %s\n", url)
	fmt.Printf("📺 Channel: %s\n", *channel)
	fmt.Printf("🎞️  Resolution: %s\n", *res)
	fmt.Println(strings.Repeat("=", 40))

	// Initial run immediately, then tick every second
	v.fetchPlaylist(url)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		v.fetchPlaylist(url)
	}
}
