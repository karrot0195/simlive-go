package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	mu   sync.Mutex
	info *PmtInfo
	set  bool
}

func (p *PmtBaseline) Check(info *PmtInfo) (baseline *PmtInfo, consistent bool, first bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.set {
		p.info = clonePmtInfo(info)
		p.set = true
		return p.info, true, true
	}
	return p.info, info.Fingerprint == p.info.Fingerprint, false
}

func clonePmtInfo(info *PmtInfo) *PmtInfo {
	if info == nil {
		return nil
	}
	cp := &PmtInfo{Fingerprint: info.Fingerprint, Raw: info.Raw}
	cp.Streams = append([]StreamInfo(nil), info.Streams...)
	return cp
}

// ---------- PMT inspection ----------

type StreamInfo struct {
	PID        int
	StreamType int
	CodecName  string
	IsSCTE35   bool
	Language   string
}

type PmtInfo struct {
	Streams     []StreamInfo
	Fingerprint string
	Raw         string
}

type ffprobeStream struct {
	Index     int    `json:"index"`
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"`
	ID        string `json:"id"`
	Tags      struct {
		Language string `json:"language"`
	} `json:"tags"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

func InspectPmt(filePath string) (*PmtInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeLimit)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_entries", "stream=index,codec_name,codec_type,id:stream_tags=language",
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

	var parsed ffprobeOutput
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("ffprobe json parse: %w", err)
	}

	info := &PmtInfo{Raw: out.String()}

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

	for _, s := range parsed.Streams {
		si := StreamInfo{
			CodecName: s.CodecName,
			Language:  s.Tags.Language,
		}
		if s.CodecName == "scte_35" {
			si.IsSCTE35 = true
		}
		si.StreamType = streamTypeFromCodec(s.CodecName, s.CodecType)
		// parse id
		id := strings.TrimSpace(s.ID)
		var pid int
		if strings.HasPrefix(strings.ToLower(id), "0x") {
			fmt.Sscanf(id, "%x", &pid)
		} else {
			fmt.Sscanf(id, "%d", &pid)
			if pid == 0 && id != "" && id != "0" {
				// try hex fallback
				fmt.Sscanf(id, "%x", &pid)
			}
		}
		si.PID = pid

		info.Streams = append(info.Streams, si)
	}

	if len(info.Streams) == 0 {
		return nil, fmt.Errorf("no streams reported")
	}

	var sb strings.Builder
	for _, s := range info.Streams {
		lang := s.Language
		if lang == "" {
			lang = "-"
		}
		fmt.Fprintf(&sb, "%04x:%02x:%s;", s.PID, s.StreamType, lang)
	}
	sum := sha256.Sum256([]byte(sb.String()))
	info.Fingerprint = hex.EncodeToString(sum[:6])

	return info, nil
}

func DiffPmt(baseline, current *PmtInfo) []string {
	var out []string
	if baseline == nil || current == nil {
		return out
	}
	// index baseline by PID
	baseByPID := make(map[int]StreamInfo)
	for _, s := range baseline.Streams {
		baseByPID[s.PID] = s
	}
	curByPID := make(map[int]StreamInfo)
	for _, s := range current.Streams {
		curByPID[s.PID] = s
	}

	for _, bs := range baseline.Streams {
		cs, ok := curByPID[bs.PID]
		if !ok {
			out = append(out, fmt.Sprintf("PID 0x%04X stream_type 0x%02X: stream only in baseline", bs.PID, bs.StreamType))
			continue
		}
		if bs.Language != cs.Language {
			out = append(out, fmt.Sprintf("PID 0x%04X stream_type 0x%02X: language %q -> %q", bs.PID, bs.StreamType, bs.Language, cs.Language))
		}
	}
	for _, cs := range current.Streams {
		if _, ok := baseByPID[cs.PID]; !ok {
			out = append(out, fmt.Sprintf("PID 0x%04X stream_type 0x%02X: stream only in current", cs.PID, cs.StreamType))
		}
	}
	return out
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
