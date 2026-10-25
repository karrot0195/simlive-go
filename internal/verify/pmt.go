package verify

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	ProbeLimit = 5 * time.Second

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

// ---------- PMT inspection ----------

type StreamInfo struct {
	PID        int
	StreamType int
	CodecName  string
	IsSCTE35   bool
}

type PmtInfo struct {
	Streams     []StreamInfo
	Fingerprint string
	Raw         string
}

func InspectPmt(filePath string) (*PmtInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeLimit)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries",
		"stream=index,codec_name,codec_type,id,codec_tag_string:stream_tags",
		"-of", "default=noprint_wrappers=1",
		filePath,
	)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("timeout")
		}
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(errBuf.String()))
	}

	info := &PmtInfo{Raw: out.String()}
	var cur *StreamInfo
	flush := func() {
		if cur != nil {
			info.Streams = append(info.Streams, *cur)
			cur = nil
		}
	}

	streamTypeFromCodec := func(codec, codecType string) int {
		switch codec {
		case "scte_35":
			return ExpectedSCTE35StreamTyp
		case "h264":
			return 0x1B
		case "hevc":
			return 0x24
		case "aac":
			return 0x0F
		case "ac3":
			return 0x81
		case "mp2":
			return 0x03
		}
		switch codecType {
		case "video":
			return 0x1B
		case "audio":
			return 0x0F
		case "data":
			return 0x15
		}
		return -1
	}

	scanner := bufio.NewScanner(strings.NewReader(out.String()))
	for scanner.Scan() {
		line := scanner.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "index":
			flush()
			cur = &StreamInfo{PID: -1, StreamType: -1}
		case "codec_name":
			if cur != nil {
				cur.CodecName = v
				if v == "scte_35" {
					cur.IsSCTE35 = true
				}
			}
		case "codec_type":
			if cur != nil {
				cur.StreamType = streamTypeFromCodec(cur.CodecName, v)
			}
		case "id":
			if cur != nil {
				var pid int
				if _, err := fmt.Sscanf(v, "%x", &pid); err == nil {
					cur.PID = pid
				} else if _, err := fmt.Sscanf(v, "%d", &pid); err == nil {
					cur.PID = pid
				}
			}
		}
	}
	flush()

	if len(info.Streams) == 0 {
		return nil, fmt.Errorf("no streams reported")
	}

	var sb strings.Builder
	for _, s := range info.Streams {
		fmt.Fprintf(&sb, "%04x:%02x;", s.PID, s.StreamType)
	}
	sum := sha256.Sum256([]byte(sb.String()))
	info.Fingerprint = hex.EncodeToString(sum[:6])

	return info, nil
}

func (p *PmtInfo) FindSCTE35() *StreamInfo {
	for i := range p.Streams {
		if p.Streams[i].IsSCTE35 || p.Streams[i].StreamType == ExpectedSCTE35StreamTyp {
			return &p.Streams[i]
		}
	}
	return nil
}

func RawPsi(filePath string, timeout time.Duration) (string, error) {
	if _, err := exec.LookPath("tsp"); err != nil {
		return "", fmt.Errorf("tsp not found in PATH (tsduck not installed?)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tsp",
		"-I", "file", filePath,
		"-P", "psi",
		"-O", "drop",
	)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timeout")
	}
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

func SummarizeRawPsi(raw string) (pmtLine string, scteLine string, cueiFound bool, regCount int) {
	regCount = 0
	for _, line := range strings.Split(raw, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "* PMT") || strings.Contains(l, "PID 0x") && strings.Contains(l, "PMT") {
			if pmtLine == "" {
				pmtLine = l
			}
		}
		if strings.Contains(l, "SCTE-35") || strings.Contains(l, "SCTE35") || strings.Contains(l, "SCTE 35") || strings.Contains(l, "stream_type: 0x86") ||
			(strings.Contains(l, "Elementary stream") && strings.Contains(l, "0x86")) {
			if scteLine == "" {
				scteLine = l
			}
		}
		if strings.Contains(l, ExpectedRegistration) {
			cueiFound = true
			regCount++
		}
	}
	return
}