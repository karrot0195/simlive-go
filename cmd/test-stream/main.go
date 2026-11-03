package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"simlive/internal/engine"
	"simlive/internal/models"
	"simlive/internal/utils"
	"simlive/internal/verify"
)

const (
	timelineKeyPrefix = "simlive:timeline:"
	minSegmentBytes   = 1000
	psiTimeout        = 5 * time.Second
)

var (
	streamTypeRe = regexp.MustCompile(`type 0x([0-9A-Fa-f]{2})`)
	pidRe        = regexp.MustCompile(`PID: 0x([0-9A-Fa-f]+)`)
)

var audioStreamTypes = map[int]bool{
	0x03: true, // MPEG-1 audio
	0x04: true, // MPEG-2 audio
	0x0F: true, // AAC ADTS
	0x11: true, // AAC LATM
	0x81: true, // AC-3
	0x87: true, // E-AC-3
}

type audioStream struct {
	StreamType int
	PID        int
	Languages  []string
}

type segmentResult struct {
	Seq     int64
	Src     string
	Local   string
	Streams []audioStream
	Status  string
	Detail  string
}

func main() {
	flagChannel := flag.String("channel", "", "channel ID (required)")
	flagRend := flag.String("rend", engine.DefaultRendition, "rendition: 360p|720p|1080p, ladder index (0-2), or upstream dir (L_0)")
	flagExpect := flag.String("expect", "eng", "expected ISO 639 audio language")
	flagLimit := flag.Int("limit", 0, "max segments to check (0 = all)")
	flagKeep := flag.Bool("keep", true, "keep downloaded segments on disk")
	flagRedownload := flag.Bool("redownload", false, "re-download even if a local copy exists")
	flagRaw := flag.Bool("raw", false, "print the raw PSI dump for failing segments")
	flagRedisAddr := flag.String("redis-addr", "", "Redis address (default: REDIS_ADDR / settings)")
	flagRedisPass := flag.String("redis-pass", "", "Redis password (default: REDIS_PASSWORD)")
	flagRedisDB := flag.Int("redis-db", 0, "Redis database index")
	flagTimeout := flag.Duration("timeout", 30*time.Second, "per-segment rclone download timeout")
	flag.Parse()

	if *flagChannel == "" {
		fmt.Fprintln(os.Stderr, "error: -channel is required")
		flag.Usage()
		os.Exit(2)
	}

	_ = godotenv.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	settings := utils.GetSettings()

	redisAddr := *flagRedisAddr
	if redisAddr == "" {
		redisAddr = settings.RedisAddr
	}
	redisPass := *flagRedisPass
	if redisPass == "" {
		redisPass = utils.GetEnv("REDIS_PASSWORD", "")
	}

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr, Password: redisPass, DB: *flagRedisDB})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fatalf("redis %s: %v", redisAddr, err)
	}

	segments, err := fetchTimeline(ctx, rdb, *flagChannel)
	if err != nil {
		fatalf("timeline %s: %v", *flagChannel, err)
	}
	if len(segments) == 0 {
		fatalf("no segments found for channel %s (key %s%s)", *flagChannel, timelineKeyPrefix, *flagChannel)
	}
	if *flagLimit > 0 && len(segments) > *flagLimit {
		segments = segments[:*flagLimit]
	}

	fmt.Printf("channel=%s redis=%s segments=%d expect=%s\n", *flagChannel, redisAddr, len(segments), *flagExpect)

	localName, err := renditionLocalName(*flagRend)
	if err != nil {
		fatalf("%v", err)
	}
	baseDir := filepath.Join(settings.TMPDir, "test-stream", *flagChannel, localName)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		fatalf("mkdir %s: %v", baseDir, err)
	}

	results := make([]segmentResult, 0, len(segments))
	for i, seg := range segments {
		if ctx.Err() != nil {
			break
		}
		res := checkSegment(ctx, seg, *flagRend, localName, baseDir, *flagExpect, *flagTimeout, *flagRedownload, *flagRaw)
		results = append(results, res)
		printResult(i+1, len(segments), res, *flagRaw)
	}

	if !*flagKeep {
		cleanup(results)
	}

	printSummary(results, *flagExpect)
}

func fetchTimeline(ctx context.Context, rdb *redis.Client, channelID string) ([]models.SegmentItem, error) {
	key := timelineKeyPrefix + channelID
	raw, err := rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	bySeq := make(map[int64]models.SegmentItem, len(raw))
	for _, item := range raw {
		var seg models.SegmentItem
		if err := json.Unmarshal([]byte(item), &seg); err != nil {
			fmt.Fprintf(os.Stderr, "skip malformed timeline member: %v\n", err)
			continue
		}
		if seg.StoragePath == "" {
			continue
		}
		bySeq[seg.Sequence] = seg
	}

	segments := make([]models.SegmentItem, 0, len(bySeq))
	for _, seg := range bySeq {
		segments = append(segments, seg)
	}
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Sequence < segments[j].Sequence
	})
	return segments, nil
}

func renditionLocalName(wanted string) (string, error) {
	if idx, ok := renditionIndex(wanted); ok {
		return engine.RENDITIONMAP[idx], nil
	}
	if wanted == "" {
		return engine.DefaultRendition, nil
	}
	return wanted, nil
}

func renditionIndex(wanted string) (int, bool) {
	for i, label := range engine.RENDITIONMAP {
		if wanted == label {
			return i, true
		}
	}
	for i := range engine.RENDITIONMAP {
		if wanted == fmt.Sprintf("%d", i) {
			return i, true
		}
	}
	return 0, false
}

func upstreamRendition(seg models.SegmentItem, wanted string) string {
	if _, ok := renditionIndex(wanted); !ok && wanted != "" {
		return wanted
	}
	idx := 0
	if i, ok := renditionIndex(wanted); ok {
		idx = i
	}
	if idx < len(seg.Renditions) && seg.Renditions[idx] != "" {
		return seg.Renditions[idx]
	}
	if idx < len(engine.ActiveLadder) {
		if seg.IsAd {
			return engine.ActiveLadder[idx].AdsDir
		}
		return engine.ActiveLadder[idx].VodDir
	}
	return engine.DefaultRendition
}

func checkSegment(ctx context.Context, seg models.SegmentItem, wanted, localName, baseDir, expect string, timeout time.Duration, redownload, raw bool) segmentResult {
	res := segmentResult{Seq: seg.Sequence}
	rend := upstreamRendition(seg, wanted)
	res.Src = strings.ReplaceAll(seg.StoragePath, "{rendition}", rend)
	res.Local = filepath.Join(baseDir, fmt.Sprintf("seg_%d.ts", seg.Sequence))

	if needsDownload(res.Local, redownload) {
		if err := downloadSegment(ctx, res.Src, res.Local, timeout); err != nil {
			res.Status = "ERROR"
			res.Detail = err.Error()
			return res
		}
	}

	psi, err := verify.RawPsi(res.Local, psiTimeout)
	if err != nil {
		res.Status = "ERROR"
		res.Detail = err.Error()
		return res
	}
	res.Streams = parseAudioStreams(psi)

	res.Status, res.Detail = evaluate(res.Streams, expect)
	if raw && res.Status != "OK" {
		res.Detail += "\n" + psi
	}
	return res
}

func needsDownload(path string, redownload bool) bool {
	if redownload {
		return true
	}
	info, err := os.Stat(path)
	return err != nil || info.Size() < minSegmentBytes
}

func downloadSegment(parent context.Context, src, dest string, timeout time.Duration) error {
	if _, err := exec.LookPath("rclone"); err != nil {
		return fmt.Errorf("rclone not found in PATH")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "rclone", "copyto", src, dest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("rclone copyto timeout after %s: %s", timeout, src)
		}
		return fmt.Errorf("rclone copyto %s: %v: %s", src, err, strings.TrimSpace(stderr.String()))
	}
	info, err := os.Stat(dest)
	if err != nil {
		return fmt.Errorf("stat %s: %v", dest, err)
	}
	if info.Size() < minSegmentBytes {
		return fmt.Errorf("downloaded file too small: %s (%d bytes)", dest, info.Size())
	}
	return nil
}

func parseAudioStreams(psi string) []audioStream {
	var streams []audioStream
	var cur audioStream
	inStream := false
	inLanguageDescriptor := false

	flush := func() {
		if inStream {
			streams = append(streams, cur)
		}
		cur = audioStream{}
		inStream = false
		inLanguageDescriptor = false
	}

	for _, line := range strings.Split(psi, "\n") {
		l := strings.TrimSpace(line)

		if strings.HasPrefix(l, "* ") {
			flush()
			continue
		}

		if strings.HasPrefix(l, "Elementary stream:") {
			flush()
			m := streamTypeRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			streamType, err := strconv.ParseInt(m[1], 16, 32)
			if err != nil || !audioStreamTypes[int(streamType)] {
				continue
			}
			cur = audioStream{StreamType: int(streamType), PID: -1}
			if pm := pidRe.FindStringSubmatch(l); pm != nil {
				if pid, err := strconv.ParseInt(pm[1], 16, 32); err == nil {
					cur.PID = int(pid)
				}
			}
			inStream = true
			continue
		}

		if inStream && strings.Contains(l, "ISO") && strings.Contains(l, "639") && strings.Contains(l, "Language") {
			inLanguageDescriptor = true
			continue
		}

		if inLanguageDescriptor && strings.HasPrefix(l, "Language:") {
			value := strings.TrimPrefix(l, "Language:")
			value = strings.SplitN(value, ",", 2)[0]
			cur.Languages = append(cur.Languages, strings.TrimSpace(value))
			inLanguageDescriptor = false
		}
	}
	flush()
	return streams
}

func evaluate(streams []audioStream, expect string) (string, string) {
	if len(streams) == 0 {
		return "NO_AUDIO", "no audio elementary stream found in PMT"
	}

	var all []string
	var missing, mismatched []string
	for _, s := range streams {
		if len(s.Languages) == 0 {
			missing = append(missing, fmt.Sprintf("pid=0x%04x type=0x%02x", s.PID, s.StreamType))
			continue
		}
		for _, lang := range s.Languages {
			all = append(all, lang)
			if lang != expect {
				mismatched = append(mismatched, fmt.Sprintf("pid=0x%04x lang=%s", s.PID, lang))
			}
		}
	}

	detail := "langs=" + strings.Join(all, ",")
	if len(missing) > 0 {
		return "MISSING", detail + " missing-descriptor: " + strings.Join(missing, " ")
	}
	if len(mismatched) > 0 {
		return "MISMATCH", detail + " expected=" + expect + " offending: " + strings.Join(mismatched, " ")
	}
	return "OK", detail
}

func printResult(n, total int, res segmentResult, raw bool) {
	switch res.Status {
	case "OK":
		fmt.Printf("[%d/%d] OK       seq=%d %s\n", n, total, res.Seq, res.Detail)
		return
	case "ERROR":
		fmt.Printf("[%d/%d] ERROR    seq=%d src=%s err=%s\n", n, total, res.Seq, res.Src, res.Detail)
		return
	}

	fmt.Printf("[%d/%d] %-8s seq=%d %s\n", n, total, res.Status, res.Seq, res.Detail)
	fmt.Printf("        src=%s\n        local=%s\n", res.Src, res.Local)
	if !raw {
		fmt.Printf("        (re-run with -raw to dump the PMT)\n")
	}
}

func printSummary(results []segmentResult, expect string) {
	counts := map[string]int{}
	var failing []int64

	for _, res := range results {
		counts[res.Status]++
		if res.Status != "OK" {
			failing = append(failing, res.Seq)
		}
	}

	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("checked=%d ok=%d mismatch=%d missing=%d no_audio=%d error=%d expected=%s\n",
		len(results), counts["OK"], counts["MISMATCH"], counts["MISSING"], counts["NO_AUDIO"], counts["ERROR"], expect)
	if len(failing) == 0 {
		fmt.Println("result: PASS - every checked segment has the expected audio language")
		return
	}
	fmt.Printf("result: FAIL - segments needing attention: %v\n", failing)
	os.Exit(1)
}

func cleanup(results []segmentResult) {
	for _, res := range results {
		if res.Local != "" {
			_ = os.Remove(res.Local)
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
