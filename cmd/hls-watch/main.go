package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	baseURL       = "http://localhost:8080/live/f1533ebe-dc74-4e85-a34e-a9bc4eedee78/360p/playlist.m3u8"
	checkInterval = 5 * time.Second
)

type HLSMonitor struct {
	client        *http.Client
	mu            sync.Mutex
	processedSeqs map[int]bool
	baseURL       string
}

type SegmentInfo struct {
	SeqNum       int
	URL          string
	Duration     float64
	SCTE35Info   SCTE35Info
	PTSInfo      PTSInfo
	PMTInfo      PMTInfo
	PlaylistTags map[string]string
}

type SCTE35Info struct {
	HasSCTE35           bool
	EventType           string
	EventID             string
	Duration            float64
	Immediate           bool
	SegmentationType    string
	SegmentationUpid    string
	SegmentationEventID string
	HasSegmentationDesc bool
	HasSpliceInsert     bool
	HasSpliceSchedule   bool
	RawData             string
}

type PTSInfo struct {
	FirstPTS        float64
	LastPTS         float64
	PTSCount        int
	PTSRange        float64
	AvgPTSGap       float64
	PTSValues       []float64
	HasPTSGap       bool
	PTSGapPositions []int
}

type PMTInfo struct {
	HasPMT         bool
	PID            string
	StreamType     string
	ElementaryPIDs []string
	ProgramNumber  int
	PCRPID         string
	Version        int
	HasSCTE35      bool
	SCTE35PID      string
	HasCUEI        bool
	Fingerprint    string
}

func NewHLSMonitor(baseURL string) *HLSMonitor {
	return &HLSMonitor{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		processedSeqs: make(map[int]bool),
		baseURL:       baseURL,
	}
}

func (m *HLSMonitor) fetchPlaylist(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", m.baseURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func (m *HLSMonitor) parsePlaylist(content string) ([]SegmentInfo, error) {
	var segments []SegmentInfo
	var currentSeq int
	var scte35Data string
	var currentDuration float64
	var playlistTags = make(map[string]string)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			fmt.Sscanf(line, "#EXT-X-MEDIA-SEQUENCE:%d", &currentSeq)
		}

		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			var duration int
			fmt.Sscanf(line, "#EXT-X-TARGETDURATION:%d", &duration)
			playlistTags["target_duration"] = line
		}

		if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") {
			playlistTags["playlist_type"] = line
		}

		if strings.HasPrefix(line, "#EXT-X-VERSION:") {
			playlistTags["version"] = line
		}

		if strings.HasPrefix(line, "#EXT-X-DATERANGE:") {
			if strings.Contains(line, "SCTE35-OUT") {
				parts := strings.Split(line, "SCTE35-OUT=")
				if len(parts) > 1 {
					scte35Data = strings.Trim(parts[1], `"`)
				}
			}
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			var duration float64
			fmt.Sscanf(line, "#EXTINF:%f,", &duration)
			currentDuration = duration
			continue
		}

		if !strings.HasPrefix(line, "#") && line != "" && strings.HasSuffix(line, ".ts") {
			seq := currentSeq
			if len(segments) > 0 {
				seq = currentSeq + len(segments)
			}

			segmentURL := line
			if !strings.HasPrefix(line, "http") {
				base, _ := url.Parse(m.baseURL)
				base.Path = filepath.Dir(base.Path) + "/" + line
				segmentURL = base.String()
			}

			seg := SegmentInfo{
				SeqNum:       seq,
				URL:          segmentURL,
				Duration:     currentDuration,
				PlaylistTags: playlistTags,
			}

			if scte35Data != "" {
				seg.SCTE35Info = m.parseSCTE35FromString(scte35Data)
				seg.SCTE35Info.RawData = scte35Data
				scte35Data = ""
			}

			segments = append(segments, seg)
		}
	}

	return segments, scanner.Err()
}

func (m *HLSMonitor) parseSCTE35FromString(data string) SCTE35Info {
	info := SCTE35Info{
		HasSCTE35: true,
	}

	if strings.Contains(data, "segmentation") || strings.Contains(data, "SEGMENTATION") {
		info.HasSegmentationDesc = true

		re := regexp.MustCompile(`segmentation_type[=:](\d+)`)
		if matches := re.FindStringSubmatch(data); len(matches) > 1 {
			info.SegmentationType = matches[1]
		}

		re = regexp.MustCompile(`segmentation_event[=:](\w+)`)
		if matches := re.FindStringSubmatch(data); len(matches) > 1 {
			info.SegmentationEventID = matches[1]
		}

		re = regexp.MustCompile(`segmentation_upid[=:]([^,]+)`)
		if matches := re.FindStringSubmatch(data); len(matches) > 1 {
			info.SegmentationUpid = matches[1]
		}
	}

	if strings.Contains(data, "splice_insert") || strings.Contains(data, "SPLICE_INSERT") {
		info.HasSpliceInsert = true
		re := regexp.MustCompile(`event_id[=:](\d+)`)
		if matches := re.FindStringSubmatch(data); len(matches) > 1 {
			info.EventID = matches[1]
		}

		if strings.Contains(data, "immediate") {
			info.Immediate = true
		}

		re = regexp.MustCompile(`duration[=:]([\d.]+)`)
		if matches := re.FindStringSubmatch(data); len(matches) > 1 {
			if d, err := strconv.ParseFloat(matches[1], 64); err == nil {
				info.Duration = d
			}
		}
	}

	if strings.Contains(data, "splice_schedule") || strings.Contains(data, "SPLICE_SCHEDULE") {
		info.HasSpliceSchedule = true
	}

	if info.HasSpliceInsert {
		info.EventType = "splice_insert"
	} else if info.HasSpliceSchedule {
		info.EventType = "splice_schedule"
	} else if info.HasSegmentationDesc {
		info.EventType = "segmentation_descriptor"
	} else {
		info.EventType = "unknown"
	}

	return info
}

// Download remote HTTP segment to a local temp file for processing by external tools
func (m *HLSMonitor) downloadOrGetLocalFile(ctx context.Context, fileURL string) (string, func(), error) {
	if !strings.HasPrefix(fileURL, "http://") && !strings.HasPrefix(fileURL, "https://") {
		return fileURL, func() {}, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return "", nil, err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, resp.Status)
	}

	tmpFile, err := os.CreateTemp("", "hls-segment-*.ts")
	if err != nil {
		return "", nil, err
	}

	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", nil, err
	}

	cleanup := func() {
		os.Remove(tmpFile.Name())
	}

	return tmpFile.Name(), cleanup, nil
}

// Analyze MPEG-TS segment using TSDuck
func (m *HLSMonitor) analyzeSegmentWithTSDuck(filePath string) (SCTE35Info, PMTInfo, error) {
	// Execute tstables on local file
	cmd := exec.Command("tstables", filePath)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil && len(outputBytes) == 0 {
		return SCTE35Info{}, PMTInfo{}, err
	}

	output := string(outputBytes)
	scte35Info := m.parseSCTE35FromOutput(output)
	pmtInfo := m.parsePMTFromOutput(output)

	return scte35Info, pmtInfo, nil
}

func (m *HLSMonitor) parseSCTE35FromOutput(output string) SCTE35Info {
	info := SCTE35Info{}
	outputLower := strings.ToLower(output)

	if !strings.Contains(outputLower, "splice") && !strings.Contains(outputLower, "0x38") && !strings.Contains(outputLower, "scte") {
		return info
	}

	info.HasSCTE35 = true

	if strings.Contains(outputLower, "splice_insert") || strings.Contains(outputLower, "splice insert") {
		info.HasSpliceInsert = true
		info.EventType = "splice_insert"
	}
	if strings.Contains(outputLower, "splice_schedule") || strings.Contains(outputLower, "splice schedule") {
		info.HasSpliceSchedule = true
		if info.EventType == "" {
			info.EventType = "splice_schedule"
		}
	}
	if strings.Contains(outputLower, "segmentation_descriptor") || strings.Contains(outputLower, "segmentation descriptor") || strings.Contains(outputLower, "segmentation") {
		info.HasSegmentationDesc = true
		if info.EventType == "" {
			info.EventType = "segmentation_descriptor"
		}
	}

	if info.EventType == "" {
		info.EventType = "unknown"
	}

	// Extract Event ID
	reEventID := regexp.MustCompile(`(?i)(?:splice\s+event\s+id|segmentation\s+event\s+id|event_id)\s*[:=]?\s*(0x[0-9a-fA-F]+|\d+)`)
	if matches := reEventID.FindStringSubmatch(output); len(matches) > 1 {
		info.EventID = matches[1]
	}

	// Extract Duration
	reDuration := regexp.MustCompile(`(?i)duration\s*[:=]?\s*([\d.]+)`)
	if matches := reDuration.FindStringSubmatch(output); len(matches) > 1 {
		if d, err := strconv.ParseFloat(matches[1], 64); err == nil {
			info.Duration = d
		}
	}

	// Extract Immediate
	if strings.Contains(outputLower, "immediate") {
		info.Immediate = true
	}

	// Extract Segmentation details
	if info.HasSegmentationDesc {
		reSegType := regexp.MustCompile(`(?i)(?:segmentation\s+type|segmentation_type)\s*[:=]?\s*(0x[0-9a-fA-F]+|\d+)`)
		if matches := reSegType.FindStringSubmatch(output); len(matches) > 1 {
			info.SegmentationType = matches[1]
		}

		reSegEventID := regexp.MustCompile(`(?i)(?:segmentation\s+event\s+id|segmentation_event_id)\s*[:=]?\s*(0x[0-9a-fA-F]+|\w+)`)
		if matches := reSegEventID.FindStringSubmatch(output); len(matches) > 1 {
			info.SegmentationEventID = matches[1]
		}

		reSegUpid := regexp.MustCompile(`(?i)(?:segmentation\s+upid|segmentation_upid)\s*[:=]?\s*([^\s,]+)`)
		if matches := reSegUpid.FindStringSubmatch(output); len(matches) > 1 {
			info.SegmentationUpid = matches[1]
		}
	}

	return info
}

func (m *HLSMonitor) parsePMTFromOutput(output string) PMTInfo {
	info := PMTInfo{Version: -1}
	outputLower := strings.ToLower(output)

	if !strings.Contains(outputLower, "pmt") && !strings.Contains(outputLower, "program map table") {
		return info
	}

	info.HasPMT = true

	// Version number
	reVersion := regexp.MustCompile(`(?i)Version:\s*(\d+)`)
	if matches := reVersion.FindStringSubmatch(output); len(matches) > 1 {
		if v, err := strconv.Atoi(matches[1]); err == nil {
			info.Version = v
		}
	}

	// Program Number
	reProgNum := regexp.MustCompile(`(?i)program\s+number\s*[:=]?\s*(\d+)`)
	if matches := reProgNum.FindStringSubmatch(output); len(matches) > 1 {
		if pn, err := strconv.Atoi(matches[1]); err == nil {
			info.ProgramNumber = pn
		}
	}

	// PMT PID
	rePMTPID := regexp.MustCompile(`(?i)(?:pmt\s+pid|pmt.*pid)\s*[:=]?\s*(0x[0-9a-fA-F]+|\d+)`)
	if matches := rePMTPID.FindStringSubmatch(output); len(matches) > 1 {
		info.PID = matches[1]
	} else {
		reHeaderPID := regexp.MustCompile(`(?i)PMT.*PID\s+(0x[0-9a-fA-F]+|\d+)`)
		if matches := reHeaderPID.FindStringSubmatch(output); len(matches) > 1 {
			info.PID = matches[1]
		}
	}

	// PCR PID
	rePCRPID := regexp.MustCompile(`(?i)pcr\s+pid\s*[:=]?\s*(0x[0-9a-fA-F]+|\d+)`)
	if matches := rePCRPID.FindStringSubmatch(output); len(matches) > 1 {
		info.PCRPID = matches[1]
	}

	// Elementary PIDs & Stream Types
	reElementary := regexp.MustCompile(`(?i)elementary\s+stream:?\s*pid\s*(0x[0-9a-fA-F]+|\d+).*?stream\s+type:?\s*(0x[0-9a-fA-F]+|\d+)`)
	matches := reElementary.FindAllStringSubmatch(output, -1)
	for _, m := range matches {
		if len(m) > 1 {
			info.ElementaryPIDs = append(info.ElementaryPIDs, m[1])
		}
		if len(m) > 2 && info.StreamType == "" {
			info.StreamType = m[2]
		}
	}

	if len(info.ElementaryPIDs) == 0 {
		reElemPID := regexp.MustCompile(`(?i)(?:elementary_pid|elementary\s+pid)\s*[:=]?\s*(0x[0-9a-fA-F]+|\d+)`)
		elemMatches := reElemPID.FindAllStringSubmatch(output, -1)
		for _, m := range elemMatches {
			if len(m) > 1 {
				info.ElementaryPIDs = append(info.ElementaryPIDs, m[1])
			}
		}

		reStreamType := regexp.MustCompile(`(?i)(?:stream_type|stream\s+type)\s*[:=]?\s*(0x[0-9a-fA-F]+|\d+)`)
		if stMatches := reStreamType.FindStringSubmatch(output); len(stMatches) > 1 {
			info.StreamType = stMatches[1]
		}
	}

	// SCTE-35 detection
	if strings.Contains(outputLower, "scte-35") || strings.Contains(outputLower, "scte35") || strings.Contains(output, "0x86") {
		info.HasSCTE35 = true
		reSCTE35PID := regexp.MustCompile(`(?i)SCTE.*?PID\s+(0x[0-9a-fA-F]+|\d+)`)
		if matches := reSCTE35PID.FindStringSubmatch(output); len(matches) > 1 {
			info.SCTE35PID = matches[1]
		}
	}

	// CUEI registration descriptor
	if strings.Contains(output, "CUEI") || strings.Contains(output, "0x43554549") {
		info.HasCUEI = true
	}

	// Compute fingerprint for consistency tracking
	var sb strings.Builder
	for _, line := range strings.Split(output, "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "stream_type") || strings.Contains(l, "Registration") || strings.Contains(l, "Format identifier") {
			fmt.Fprintf(&sb, "%s;", l)
		}
	}
	info.Fingerprint = sb.String()

	return info
}

func (m *HLSMonitor) extractPTSWithFFprobe(filePath string) PTSInfo {
	info := PTSInfo{}

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "packet=pts_time",
		"-select_streams", "v:0",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"-i", filePath)

	output, err := cmd.Output()
	if err != nil {
		return info
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var ptsValues []float64

	for _, line := range lines {
		if line == "" {
			continue
		}
		pts, err := strconv.ParseFloat(line, 64)
		if err == nil {
			ptsValues = append(ptsValues, pts)
			info.PTSCount++
		}
	}

	if len(ptsValues) > 0 {
		info.FirstPTS = ptsValues[0]
		info.LastPTS = ptsValues[len(ptsValues)-1]
		info.PTSRange = info.LastPTS - info.FirstPTS

		if len(ptsValues) > 10 {
			info.PTSValues = ptsValues[:10]
		} else {
			info.PTSValues = ptsValues
		}

		if len(ptsValues) > 1 {
			var totalGap float64
			var gaps []float64
			for i := 1; i < len(ptsValues) && i < 100; i++ {
				gap := ptsValues[i] - ptsValues[i-1]
				totalGap += gap
				gaps = append(gaps, gap)

				if gap > 1.0 {
					info.HasPTSGap = true
					info.PTSGapPositions = append(info.PTSGapPositions, i)
				}
			}
			if len(gaps) > 0 {
				info.AvgPTSGap = totalGap / float64(len(gaps))
			}
		}
	}

	return info
}

func (m *HLSMonitor) analyzeSegment(ctx context.Context, seg *SegmentInfo) {
	fmt.Printf("\n🔍 Analyzing Segment %d: %s\n", seg.SeqNum, seg.URL)
	fmt.Println(strings.Repeat("─", 80))

	// 1. Fetch segment to temporary local file
	localPath, cleanup, err := m.downloadOrGetLocalFile(ctx, seg.URL)
	if err != nil {
		fmt.Printf("❌ Error fetching segment: %v\n", err)
		return
	}
	defer cleanup()

	// 2. Get PTS Info
	seg.PTSInfo = m.extractPTSWithFFprobe(localPath)

	// 3. Get SCTE-35 and PMT info using TSDuck
	scte35, pmt, _ := m.analyzeSegmentWithTSDuck(localPath)
	seg.SCTE35Info = scte35
	seg.PMTInfo = pmt

	// Display results
	m.displayResults(seg)
}

func (m *HLSMonitor) displayResults(seg *SegmentInfo) {
	fmt.Printf("📦 Segment #%d (Duration: %.2fs)\n", seg.SeqNum, seg.Duration)

	if seg.SCTE35Info.HasSCTE35 {
		fmt.Println("\n🎯 SCTE-35 Information:")
		fmt.Printf("   Event Type: %s\n", seg.SCTE35Info.EventType)

		if seg.SCTE35Info.EventID != "" {
			fmt.Printf("   Event ID: %s\n", seg.SCTE35Info.EventID)
		}

		if seg.SCTE35Info.Duration > 0 {
			fmt.Printf("   Duration: %.2fs\n", seg.SCTE35Info.Duration)
		}

		if seg.SCTE35Info.Immediate {
			fmt.Println("   ✅ Immediate (cue-out at current PTS)")
		}

		if seg.SCTE35Info.HasSegmentationDesc {
			fmt.Println("   ✅ Has Segmentation Descriptor")
			if seg.SCTE35Info.SegmentationType != "" {
				segType := seg.SCTE35Info.SegmentationType
				segTypeDesc := getSegmentationTypeDesc(segType)
				fmt.Printf("   Segmentation Type: %s (%s)\n", segType, segTypeDesc)
			}
			if seg.SCTE35Info.SegmentationEventID != "" {
				fmt.Printf("   Segmentation Event ID: %s\n", seg.SCTE35Info.SegmentationEventID)
			}
			if seg.SCTE35Info.SegmentationUpid != "" {
				fmt.Printf("   Segmentation UPID: %s\n", seg.SCTE35Info.SegmentationUpid)
			}
		} else {
			fmt.Println("   ❌ MISSING Segmentation Descriptor")
		}

		if seg.SCTE35Info.HasSpliceInsert {
			fmt.Println("   ✅ Has Splice Insert")
		}

		if seg.SCTE35Info.HasSpliceSchedule {
			fmt.Println("   ✅ Has Splice Schedule")
		}
	} else {
		fmt.Println("\n📌 No SCTE-35 data found in this segment")
	}

	if seg.PTSInfo.PTSCount > 0 {
		fmt.Println("\n⏱️  PTS Information:")
		fmt.Printf("   Total Frames: %d\n", seg.PTSInfo.PTSCount)
		fmt.Printf("   First PTS: %.6fs\n", seg.PTSInfo.FirstPTS)
		fmt.Printf("   Last PTS: %.6fs\n", seg.PTSInfo.LastPTS)
		fmt.Printf("   PTS Range: %.6fs\n", seg.PTSInfo.PTSRange)
		fmt.Printf("   Avg PTS Gap: %.6fs\n", seg.PTSInfo.AvgPTSGap)

		if len(seg.PTSInfo.PTSValues) > 0 {
			var ptsStr []string
			for i, pts := range seg.PTSInfo.PTSValues {
				if i >= 5 {
					ptsStr = append(ptsStr, "...")
					break
				}
				ptsStr = append(ptsStr, fmt.Sprintf("%.6f", pts))
			}
			fmt.Printf("   Sample PTS: %s\n", strings.Join(ptsStr, ", "))
		}

		if seg.PTSInfo.HasPTSGap {
			fmt.Printf("   ⚠️  PTS Gaps Detected at positions: %v\n", seg.PTSInfo.PTSGapPositions)
		}

		if seg.Duration > 0 && seg.PTSInfo.PTSRange < seg.Duration-0.5 {
			fmt.Printf("   ⚠️  PTS Range (%.6fs) < Segment Duration (%.6fs)\n",
				seg.PTSInfo.PTSRange, seg.Duration)
			fmt.Println("   → Pre-roll issue: Should use ads_pts_offset - duration or current PTS")
		}

		if seg.PTSInfo.PTSCount > 0 && seg.Duration > 0 {
			expectedPTS := seg.PTSInfo.FirstPTS + seg.Duration
			if seg.PTSInfo.LastPTS > 0 && abs(seg.PTSInfo.LastPTS-expectedPTS) > 0.1 {
				fmt.Printf("   ⚠️  PTS inaccurate: Expected %.6fs, Got %.6fs (diff: %.6fs)\n",
					expectedPTS, seg.PTSInfo.LastPTS, abs(seg.PTSInfo.LastPTS-expectedPTS))
				fmt.Println("   → Should compute PTS using PTS offset during FFmpeg header rewrite")
			}
		}
	}

	if seg.PMTInfo.HasPMT {
		fmt.Println("\n📡 PMT Information:")
		fmt.Printf("   Program Number: %d\n", seg.PMTInfo.ProgramNumber)
		fmt.Printf("   PMT PID: %s\n", seg.PMTInfo.PID)
		if seg.PMTInfo.Version >= 0 {
			fmt.Printf("   Version: %d\n", seg.PMTInfo.Version)
		}
		if seg.PMTInfo.StreamType != "" {
			fmt.Printf("   Stream Type: %s\n", seg.PMTInfo.StreamType)
		}
		if seg.PMTInfo.PCRPID != "" {
			fmt.Printf("   PCR PID: %s\n", seg.PMTInfo.PCRPID)
		}
		if len(seg.PMTInfo.ElementaryPIDs) > 0 {
			fmt.Printf("   Elementary PIDs: %v\n", seg.PMTInfo.ElementaryPIDs)
		}
		if seg.PMTInfo.HasSCTE35 {
			fmt.Printf("   ✅ SCTE-35 stream: PID %s, stream_type 0x86\n", seg.PMTInfo.SCTE35PID)
		}
		if seg.PMTInfo.HasCUEI {
			fmt.Printf("   ✅ Registration descriptor CUEI present\n")
		}
	} else {
		fmt.Println("\n⚠️  NO PMT Found - Missing PMT configuration for slice points")
	}

	fmt.Println("\n🔎 Issue Summary:")
	issuesFound := false

	if seg.SCTE35Info.HasSCTE35 && !seg.SCTE35Info.HasSegmentationDesc {
		fmt.Println("   ❌ Missing segmentation_descriptor for ad events")
		issuesFound = true
	}

	if !seg.PMTInfo.HasPMT && seg.SCTE35Info.HasSCTE35 {
		fmt.Println("   ❌ Missing PMT configuration for slice points")
		issuesFound = true
	}

	if seg.Duration <= 8.0 && seg.SCTE35Info.HasSCTE35 {
		if seg.PTSInfo.PTSCount > 0 && seg.PTSInfo.PTSRange < seg.Duration-0.1 {
			fmt.Printf("   ⚠️  Pre-roll PTS timing issue: Range %.6fs < Duration %.6fs\n",
				seg.PTSInfo.PTSRange, seg.Duration)
			issuesFound = true
		}
	}

	if seg.PTSInfo.PTSCount > 0 && seg.Duration > 0 {
		expectedPTS := seg.PTSInfo.FirstPTS + seg.Duration
		if seg.PTSInfo.LastPTS > 0 && abs(seg.PTSInfo.LastPTS-expectedPTS) > 0.1 {
			fmt.Printf("   ⚠️  PTS accuracy issue: Expected %.6fs, Got %.6fs\n",
				expectedPTS, seg.PTSInfo.LastPTS)
			issuesFound = true
		}
	}

	if !issuesFound {
		fmt.Println("   ✅ No issues detected")
	}

	fmt.Println(strings.Repeat("─", 80))
}

func getSegmentationTypeDesc(segType string) string {
	types := map[string]string{
		"0x00": "Reserved",
		"0x01": "Content Identification",
		"0x10": "Program Start",
		"0x11": "Program End",
		"0x12": "Program Early Termination",
		"0x13": "Program Breakaway",
		"0x14": "Program Resumption",
		"0x15": "Program Runover",
		"0x20": "Provider Placement Opportunity",
		"0x21": "Provider Ad Start (cue-in)",
		"0x22": "Provider Ad End (cue-out)",
		"0x23": "Provider Ad Break",
		"0x30": "Distributor Placement Opportunity",
		"0x31": "Distributor Ad Start",
		"0x32": "Distributor Ad End",
		"0x33": "Distributor Ad Break",
	}

	segTypeClean := strings.ToLower(strings.TrimSpace(segType))
	if !strings.HasPrefix(segTypeClean, "0x") {
		if val, err := strconv.Atoi(segTypeClean); err == nil {
			segTypeClean = fmt.Sprintf("0x%02x", val)
		}
	}

	if desc, ok := types[segTypeClean]; ok {
		return desc
	}
	return "Unknown"
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func (m *HLSMonitor) processSegments(ctx context.Context, segments []SegmentInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range segments {
		seg := &segments[i]

		if m.processedSeqs[seg.SeqNum] {
			continue
		}

		m.analyzeSegment(ctx, seg)
		m.processedSeqs[seg.SeqNum] = true
	}
}

func (m *HLSMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	fmt.Printf("\n🚀 HLS Monitor Started\n")
	fmt.Printf("📡 URL: %s\n", m.baseURL)
	fmt.Printf("⏱️  Check Interval: %v\n", checkInterval)
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println(strings.Repeat("=", 80))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			playlist, err := m.fetchPlaylist(ctx)
			if err != nil {
				fmt.Printf("❌ Error fetching playlist: %v\n", err)
				continue
			}

			segments, err := m.parsePlaylist(playlist)
			if err != nil {
				fmt.Printf("❌ Error parsing playlist: %v\n", err)
				continue
			}

			if len(segments) > 0 {
				m.processSegments(ctx, segments)
			}
		}
	}
}

func main() {
	deps := map[string]string{
		"tstables": "tsduck",
		"ffprobe":  "ffmpeg",
	}
	missing := false

	fmt.Println("🔧 Checking dependencies...")
	for cmd, pkg := range deps {
		if _, err := exec.LookPath(cmd); err != nil {
			fmt.Printf("⚠️  %s not found. Install %s:\n", cmd, pkg)
			fmt.Printf("   macOS: brew install %s\n", pkg)
			fmt.Printf("   Linux: apt-get install %s\n", pkg)
			fmt.Printf("   Windows: download from official site\n")
			missing = true
		} else {
			fmt.Printf("✅ %s found\n", cmd)
		}
	}

	if missing {
		os.Exit(1)
	}
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	m3u8Url := flag.String("url", baseURL, "m3u8 url")
	flag.Parse()
	monitor := NewHLSMonitor(*m3u8Url)
	monitor.Run(ctx)
}
