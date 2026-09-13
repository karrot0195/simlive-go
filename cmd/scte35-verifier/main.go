package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// verifier keeps state across ticks so we only run tsp on each
// segment the first time it shows up in the live playlist.
type verifier struct {
	// seenPreroll maps segment filename -> last stdout printed
	seenPreroll map[string]string
}

func newVerifier() *verifier {
	return &verifier{seenPreroll: map[string]string{}}
}

func runTsp(segURL string) (string, error) {
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

func (v *verifier) fetchPlaylist(url string) {
	// http://host/live/<id>/<rend>/*.m3u8 -> http://host/live/<id>/<rend>/
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

		// Skip non-segment lines (comments, empty lines, master tags)
		if strings.HasPrefix(line, "#") || !strings.HasSuffix(line, ".ts") {
			continue
		}

		// Skip segments already processed on previous ticks
		if _, ok := v.seenPreroll[line]; ok {
			// Reset single-segment flags even on skip
			isPreroll = false
			isCueOut = false

			continue
		}

		segURL := dirUrlStr + "/" + line
		output, err := runTsp(segURL)
		v.seenPreroll[line] = output

		if err != nil {
			fmt.Printf("tsp failed for %s: %v\n", segURL, err)
			isPreroll = false
			continue
		}

		// Determine label type based on current state
		var label string
		if isPreroll {
			label = "Preroll"
		} else if isCueOut {
			label = "CueOut"
		} else if isAds {
			label = "Ads"
		} else {
			continue
		}

		fmt.Printf("[%s] %s\n", label, segURL)
		fmt.Println(output)
		fmt.Println("-----------------------------------------------------------------")

		// Reset non-sticky per-segment flags
		isPreroll = false
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("scanner error:", err)
	}
}

func main() {
	fmt.Println("welcome to verify tool")

	url := flag.String("url", "http://localhost:8080/live/7261da4b-9c45-4619-bfb0-a6119dc7b6e6/360p/playlist.m3u8", "url for verify")
	flag.Parse()

	v := newVerifier()

	// Initial run immediately, then tick every second
	v.fetchPlaylist(*url)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		v.fetchPlaylist(*url)
	}
}
