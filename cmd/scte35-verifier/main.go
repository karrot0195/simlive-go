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
// pre-roll segment the first time it shows up in the live playlist.
type verifier struct {
	// seenPreroll maps segment filename -> last splicemonitor stdout we
	// already printed for it. Segments in the playlist that we already
	// processed are skipped on subsequent ticks.
	seenPreroll map[string]string
}

func newVerifier() *verifier {
	return &verifier{seenPreroll: map[string]string{}}
}

// runTsp shells out to tsduck's `tsp` to scan a single MPEG-TS segment for
// splice commands. We use `-O file -` (write to stdout) instead of
// `-O drop` so we can capture the splicemonitor output, and pipe it
// through `head -n 15` to keep the per-segment dump bounded.
func runTsp(segURL string) (string, error) {
	cmd := exec.Command(
		"sh", "-c",
		fmt.Sprintf(
			// "tsp -I http %q -P splicemonitor --all-commands -O file - 2>&1 | head -n 15",
			"tsp -I http %q -P tables --no-duplicate -O drop",
			segURL,
		),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (v *verifier) fetchPlaylist(url string) {
	// http://host/live/<id>/<rend>/*.m3u8 -> http://host/live/<id>/<rend>/
	parts := strings.Split(url, "/")
	dirUrl := parts[:len(parts)-1]         // split by / and remove the last
	dirUrlStr := strings.Join(dirUrl, "/") // join to string

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

	IsPreroll := false
	IsCueOut := false
	IsAds := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "EXT-X-SCTE35:CUE") && strings.Contains(line, "TYPE=48") {
			IsPreroll = true
		}

		if strings.Contains(line, "EXT-X-SCTE35:CUE") && strings.Contains(line, "TYPE=52") {
			IsCueOut = true
			IsAds = true
		}

		if strings.Contains(line, "EXT-X-SCTE35:CUE") && strings.Contains(line, "TYPE=53") {
			IsAds = false
		}

		if !strings.HasSuffix(line, ".ts") || IsAds || IsPreroll {
			continue
		}

		if _, ok := v.seenPreroll[line]; ok {
			// already processed on a previous tick
			continue
		}

		segURL := dirUrlStr + "/" + line
		output, err := runTsp(segURL)
		v.seenPreroll[line] = output
		if err != nil {
			fmt.Printf("tsp failed for %s: %v\n", segURL, err)
			continue
		}
		label := ""

		if IsPreroll {
			label = "Preroll"
		} else if IsCueOut {
			label = "CueOut"
		} else {
			label = "Ad"
		}

		fmt.Printf("%s: %s \n", segURL, label)
		fmt.Println(output)
		fmt.Println("-----------------------------------------------------------------")

		IsPreroll = false
		IsCueOut = false
	}
	if err := scanner.Err(); err != nil {
		fmt.Println("scanner error:", err)
	}
}

func main() {
	fmt.Println("welcome to verify tool")

	url := flag.String("url", "https://simlive3.espx.cloud/live/246ff51b-8e0b-490b-97fb-92a4e21677de/playout.m3u8", "url for verify")

	flag.Parse()

	v := newVerifier()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			v.fetchPlaylist(*url)
		}
	}

}
