package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

const (
	WindowItemSize      = 50
	AdInsertionInterval = 30 // seconds
	RedisExpiryHours    = 26
	WindowBufferSeconds = 120
)

// Các rendition có thể có của VOD và ads. Thứ tự ưu tiên từ thấp đến cao
// (ưu tiên chọn cặp có resolution bằng nhau, cao nhất có thể).
var (
	vodRenditionCandidates = []string{"L_0", "L_1", "L_2", "L_3"}
	adsRenditionCandidates = []string{"L0", "L1", "L2"}
)

var ErrEndOfTimeline = errors.New("end of timeline reached - processing complete")

type renditionPair struct {
	vod string
	ads string
}

// qualityTier: cấp chất lượng theo SIMLIVE_QUALITY.
type qualityTier struct {
	label string
	w, h  int // resolution mục tiêu (width x height)
}

// qualityTierMap: SIMLIVE_QUALITY -> resolution mục tiêu.
var qualityTierMap = map[string]qualityTier{
	"360p":  {label: "360p", w: 640, h: 360},
	"720p":  {label: "720p", w: 1280, h: 720},
	"1080p": {label: "1080p", w: 1920, h: 1080},
}

// fallbackRenditionForTier: cặp VOD/ads mặc định đã verify cho từng tier khi
// không probe được rendition nào (360p->L_0/L2, 720p->L_1/L1, 1080p->L_2/L0).
var fallbackRenditionForTier = map[string]renditionPair{
	"360p":  {vod: "L_0", ads: "L2"},
	"720p":  {vod: "L_1", ads: "L1"},
	"1080p": {vod: "L_2", ads: "L0"},
}

// resolveQualityTier đọc SIMLIVE_QUALITY: auto (mặc định) | 360p | 720p | 1080p.
// Trả về tier đã chọn và isAuto. Giá trị không hợp lệ -> WARN + dùng auto.
func (p *Planner) resolveQualityTier() (tier qualityTier, isAuto bool) {
	q := utils.GetEnv("SIMLIVE_QUALITY", "auto")
	if q == "" || q == "auto" {
		return qualityTier{}, true
	}
	if t, ok := qualityTierMap[q]; ok {
		return t, false
	}
	log.Printf("[Planner] WARN: SIMLIVE_QUALITY=%q không hợp lệ (auto|360p|720p|1080p), dùng auto", q)
	return qualityTier{}, true
}

type Planner struct {
	db         *gorm.DB
	rdb        *redis.Client
	espx       *api.ESPXClient
	rclone     *storage.RcloneService
	bucketName string
	adsBucket  string

	globalRunningTime  float64
	totalSegmentsAdded int64
	channelID          string
	processStartTime   time.Time

	// Rendition matching: đảm bảo VOD và ads cùng resolution để stream liền mạch
	rendMu     sync.Mutex
	rendCache  map[string]renditionPair
	adsRend    string // resolved một lần (ads dùng chung cho mọi stream)
	adsRendSet bool
}

type SegmentProcessor struct {
	planner         *Planner
	pipe            redis.Pipeliner
	ctx             context.Context
	sequenceCounter int64
	lastAdTime      float64
	totalAdsAdded   int
	nowUnix         float64
	adSegments      []models.SegmentItem

	// PTS mapping state cho stream hiện tại
	currentPtsOffset float64
	// Mốc timeline cục bộ: MediaStart của segment đầu tiên trong timeline.
	// Mọi segment được gán PtsOffset = MediaStart - timelineBase (giây cục bộ).
	timelineBase float64
}

func NewPlanner(db *gorm.DB, rdb *redis.Client, espx *api.ESPXClient, rclone *storage.RcloneService) *Planner {
	return &Planner{
		db:         db,
		rdb:        rdb,
		espx:       espx,
		rclone:     rclone,
		bucketName: "espx-video-lib:espx-video-lib",
		adsBucket:  "espx-video-lib:espx-video-lib/ads-v3",
		rendCache:  make(map[string]renditionPair),
	}
}

// --- Rendition resolution -------------------------------------------------

// probeResolution tải segment đầu tiên của một rendition về tạm và đọc
// resolution video (width x height) bằng ffprobe. Trả về lỗi nếu rendition
// không tồn tại / không đọc được.
func (p *Planner) probeResolution(remotePath string) (w, h int, err error) {
	tmp, err := os.CreateTemp("", "simlive-probe-*.ts")
	if err != nil {
		log.Printf("[Planner] probe %s: CreateTemp failed: %v", remotePath, err)
		return 0, 0, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := p.rclone.CopyTo(remotePath, tmpPath); err != nil {
		log.Printf("[Planner] probe %s: rclone copy failed: %v", remotePath, err)
		return 0, 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0", tmpPath)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[Planner] probe %s: ffprobe failed: %v", remotePath, err)
		return 0, 0, fmt.Errorf("ffprobe: %w", err)
	}
	// ffprobe có thể in resolution 2 lần (mpegts multi-program), chỉ lấy dòng đầu
	firstLine := strings.TrimSpace(strings.Split(strings.TrimSpace(string(out)), "\n")[0])
	parts := strings.Split(firstLine, ",")
	if len(parts) != 2 {
		log.Printf("[Planner] probe %s: unexpected ffprobe output: %q", remotePath, out)
		return 0, 0, fmt.Errorf("unexpected ffprobe output: %q", out)
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("bad resolution %q", out)
	}
	return w, h, nil
}

// probeRenditions đo resolution của từng rendition trong danh sách.
func (p *Planner) probeRenditions(basePath, firstSeg string, candidates []string) map[string][2]int {
	res := make(map[string][2]int)
	for _, dir := range candidates {
		remote := fmt.Sprintf("%s/%s/%s", basePath, dir, firstSeg)
		w, h, err := p.probeResolution(remote)
		if err == nil {
			res[dir] = [2]int{w, h}
		}
	}
	return res
}

// resolveAdsRendition chọn rendition của ads theo thứ tự: env
// SIMLIVE_ADS_RENDITION (ép cứng) > tier SIMLIVE_QUALITY (rendition gần
// resolution mục tiêu nhất) > auto (rendition lớn nhất đo được). Kết quả được
// cache một lần vì ads dùng chung cho mọi stream.
func (p *Planner) resolveAdsRendition() string {
	if p.adsRendSet {
		return p.adsRend
	}
	defer func() { p.adsRendSet = true }()

	if ex := utils.GetEnv("SIMLIVE_ADS_RENDITION", ""); ex != "" {
		p.adsRend = ex
		return p.adsRend
	}

	probes := p.probeRenditions(p.adsBucket, "vod_000.ts", adsRenditionCandidates)
	tier, isAuto := p.resolveQualityTier()
	if !isAuto {
		if best, ok := pickClosestRendition(probes, tier.w, tier.h); ok {
			p.adsRend = best
			return p.adsRend
		}
		if fb, ok := fallbackRenditionForTier[tier.label]; ok {
			log.Printf("[Planner] WARN: không probe được resolution ads cho tier %s, fallback %s", tier.label, fb.ads)
			p.adsRend = fb.ads
			return p.adsRend
		}
	}

	best := ""
	bestArea := -1
	for dir, wh := range probes {
		if area := wh[0] * wh[1]; area > bestArea {
			best, bestArea = dir, area
		}
	}
	if best == "" {
		log.Printf("[Planner] WARN: không probe được resolution ads, fallback L0")
		best = "L0"
	}
	p.adsRend = best
	return best
}

// resolveRenditions chọn cặp rendition (VOD, ads) cho streamID theo tier
// SIMLIVE_QUALITY: ưu tiên cặp VOD==ads có resolution đúng tier mục tiêu; nếu
// không có thì chọn cặp VOD==ads gần tier nhất (VOD==ads là quan trọng nhất
// cho playback liền mạch). Auto: cặp resolution bằng nhau cao nhất. Có thể ép
// bằng env SIMLIVE_VOD_RENDITION / SIMLIVE_ADS_RENDITION.
func (p *Planner) resolveRenditions(streamID string) renditionPair {
	p.rendMu.Lock()
	defer p.rendMu.Unlock()
	if pair, ok := p.rendCache[streamID]; ok {
		return pair
	}

	pair := renditionPair{vod: "L_0", ads: "L0"} // fallback cũ nếu mọi thứ fail
	vodExplicit := utils.GetEnv("SIMLIVE_VOD_RENDITION", "")
	adsExplicit := utils.GetEnv("SIMLIVE_ADS_RENDITION", "")

	if vodExplicit != "" && adsExplicit != "" {
		pair = renditionPair{vod: vodExplicit, ads: adsExplicit}
		p.rendCache[streamID] = pair
		return pair
	}

	vodBase := fmt.Sprintf("%s/%s", p.bucketName, streamID)
	vodProbes := p.probeRenditions(vodBase, "vod_000.ts", vodRenditionCandidates)
	adsProbes := p.probeRenditions(p.adsBucket, "vod_000.ts", adsRenditionCandidates)
	tier, isAuto := p.resolveQualityTier()

	if len(vodProbes) == 0 || len(adsProbes) == 0 {
		log.Printf("[Planner] WARN: không probe được rendition cho stream %s (vod=%d ads=%d)",
			streamID, len(vodProbes), len(adsProbes))
		if !isAuto {
			if fb, ok := fallbackRenditionForTier[tier.label]; ok {
				pair = fb
				log.Printf("[Planner] WARN: stream %s fallback theo tier %s: VOD %s + ads %s",
					streamID, tier.label, pair.vod, pair.ads)
				p.rendCache[streamID] = pair
				return pair
			}
		}
		p.rendCache[streamID] = pair
		return pair
	}

	if !isAuto {
		if tierPair, exact, ok := pickPairForTier(vodProbes, adsProbes, tier.w, tier.h); ok {
			pair = tierPair
			if !exact {
				log.Printf("[Planner] WARN: stream %s không có cặp VOD/ads khớp tier %s (%dx%d), chọn VOD %s (%dx%d) + ads %s (%dx%d) gần nhất",
					streamID, tier.label, tier.w, tier.h,
					pair.vod, vodProbes[pair.vod][0], vodProbes[pair.vod][1],
					pair.ads, adsProbes[pair.ads][0], adsProbes[pair.ads][1])
			}
			// ads rendition thực tế được chọn toàn cục (resolveAdsRendition) -
			// log và cảnh báo nếu lệch resolution với VOD đã chọn.
			adsRend := p.resolveAdsRendition()
			if aWh, ok := adsProbes[adsRend]; ok {
				vWh := vodProbes[pair.vod]
				if vWh[0] != aWh[0] || vWh[1] != aWh[1] {
					log.Printf("[Planner] WARN: stream %s VOD %s (%dx%d) lệch resolution với ads %s (%dx%d); cân nhắc ép SIMLIVE_VOD_RENDITION/SIMLIVE_ADS_RENDITION",
						streamID, pair.vod, vWh[0], vWh[1], adsRend, aWh[0], aWh[1])
				}
			}
			log.Printf("[Planner] quality tier=%s, stream %s -> renditions: VOD %s + ads %s", tier.label, streamID, pair.vod, adsRend)
			p.rendCache[streamID] = pair
			return pair
		}
		log.Printf("[Planner] WARN: stream %s không có probe rendition nào cho tier %s", streamID, tier.label)
	}

	// 1) Auto (hoặc tier không khớp): ưu tiên cặp resolution bằng nhau, cao nhất
	bestPair := renditionPair{}
	bestArea := -1
	for vDir, vWh := range vodProbes {
		for aDir, aWh := range adsProbes {
			if vWh[0] == aWh[0] && vWh[1] == aWh[1] {
				if area := vWh[0] * vWh[1]; area > bestArea {
					bestArea = area
					bestPair = renditionPair{vod: vDir, ads: aDir}
				}
			}
		}
	}
	if bestArea >= 0 {
		pair = bestPair
	} else {
		// 2) Không có cặp bằng nhau: chọn cặp lệch diện tích ít nhất
		diff := int(^uint(0) >> 1) // MaxInt
		for vDir, vWh := range vodProbes {
			for aDir, aWh := range adsProbes {
				if d := abs(vWh[0]*vWh[1] - aWh[0]*aWh[1]); d < diff {
					diff = d
					pair = renditionPair{vod: vDir, ads: aDir}
				}
			}
		}
		log.Printf("[Planner] WARN: stream %s không có cặp rendition cùng resolution, chọn VOD %s + ads %s (lệch ít nhất)",
			streamID, pair.vod, pair.ads)
	}

	log.Printf("[Planner] stream %s -> renditions: VOD %s + ads %s", streamID, pair.vod, pair.ads)
	p.rendCache[streamID] = pair
	return pair
}

// pickClosestRendition chọn rendition có resolution gần target nhất (theo diện tích).
func pickClosestRendition(probes map[string][2]int, tw, th int) (string, bool) {
	best := ""
	bestDiff := int(^uint(0) >> 1) // MaxInt
	for dir, wh := range probes {
		if d := abs(wh[0]*wh[1] - tw*th); d < bestDiff {
			bestDiff = d
			best = dir
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

// pickPairForTier chọn cặp (VOD, ads) cho tier mục tiêu:
//  1. Cặp VOD==ads có resolution đúng tier mục tiêu -> exact=true.
//  2. Không có: cặp VOD==ads gần tier nhất theo diện tích -> exact=false.
//  3. Không có cặp VOD==ads nào: cặp lệch diện tích ít nhất -> exact=false.
//
// ok=false chỉ khi không có probe nào.
func pickPairForTier(vodProbes, adsProbes map[string][2]int, tw, th int) (pair renditionPair, exact bool, ok bool) {
	// 1) Đúng tier
	for vDir, vWh := range vodProbes {
		for aDir, aWh := range adsProbes {
			if vWh[0] == tw && vWh[1] == th && aWh[0] == tw && aWh[1] == th {
				return renditionPair{vod: vDir, ads: aDir}, true, true
			}
		}
	}

	// 2) VOD==ads, gần tier nhất
	bestPair := renditionPair{}
	bestDiff := int(^uint(0) >> 1) // MaxInt
	for vDir, vWh := range vodProbes {
		for aDir, aWh := range adsProbes {
			if vWh[0] != aWh[0] || vWh[1] != aWh[1] {
				continue
			}
			if d := abs(vWh[0]*vWh[1] - tw*th); d < bestDiff {
				bestDiff = d
				bestPair = renditionPair{vod: vDir, ads: aDir}
			}
		}
	}
	if bestDiff != int(^uint(0)>>1) {
		return bestPair, false, true
	}

	// 3) Không có cặp VOD==ads: cặp lệch diện tích ít nhất
	diff := int(^uint(0) >> 1) // MaxInt
	for vDir, vWh := range vodProbes {
		for aDir, aWh := range adsProbes {
			if d := abs(vWh[0]*vWh[1] - aWh[0]*aWh[1]); d < diff {
				diff = d
				bestPair = renditionPair{vod: vDir, ads: aDir}
			}
		}
	}
	if diff == int(^uint(0)>>1) {
		return renditionPair{}, false, false
	}
	return bestPair, false, true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// loadAdSegments cập nhật để parse PTS cho ads nếu cần (hoặc dùng duration chuẩn)
func (p *Planner) loadAdSegments() ([]models.SegmentItem, error) {
	adsRend := p.resolveAdsRendition()
	adsM3u8Path := fmt.Sprintf("%s/%s/vod.m3u8", p.adsBucket, adsRend)
	m3u8Content, err := p.rclone.Cat(adsM3u8Path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ads m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var adSegments []models.SegmentItem
	var currentDuration float64
	var ptsStart, ptsEnd int64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			if dur, err := strconv.ParseFloat(durStr, 64); err == nil {
				currentDuration = dur
			}
		case strings.HasPrefix(line, "#EXT-X-PTS-RANGE:"):
			ptsRange := strings.TrimPrefix(line, "#EXT-X-PTS-RANGE:")
			parts := strings.Split(ptsRange, "-")
			if len(parts) == 2 {
				ptsStart, _ = strconv.ParseInt(parts[0], 10, 64)
				ptsEnd, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		case strings.HasSuffix(line, ".ts"):
			mStart := float64(ptsStart) / 90000.0
			mEnd := float64(ptsEnd) / 90000.0
			if currentDuration == 0 {
				currentDuration = mEnd - mStart
			}
			adSegments = append(adSegments, models.SegmentItem{
				Duration:    currentDuration,
				StoragePath: fmt.Sprintf("%s/%s/%s", p.adsBucket, adsRend, line),
				PtsStart:    ptsStart,
				PtsEnd:      ptsEnd,
				MediaStart:  mStart,
				MediaEnd:    mEnd,
			})
			currentDuration = 0
		}
	}
	if len(adSegments) > 5 {
		return adSegments[:5], nil
	}
	return adSegments, nil
}

func (p *Planner) getRedisKey() string {
	return fmt.Sprintf("simlive:timeline:%s", p.channelID)
}

func (p *Planner) getLastSequenceNumber(ctx context.Context) int64 {
	items, err := p.rdb.ZRevRange(ctx, p.getRedisKey(), 0, 0).Result()
	if err != nil || len(items) == 0 {
		return 0
	}
	var seg models.SegmentItem
	if err := json.Unmarshal([]byte(items[0]), &seg); err == nil {
		return seg.Sequence
	}
	return 0
}

func (p *Planner) ProcessEPGToTimeline(ctx context.Context, channel models.PlayoutChannel, dateStr string) error {
	p.channelID = channel.ID
	p.processStartTime = time.Now()

	schedules, err := p.espx.GetSchedules(channel.ChannelEpgID, dateStr, configs.TZ_NUM)
	if err != nil || len(schedules.Data) == 0 {
		return err
	}

	adSegments, _ := p.loadAdSegments()
	lastSeq := p.getLastSequenceNumber(ctx)

	processor := &SegmentProcessor{
		planner:         p,
		pipe:            p.rdb.Pipeline(),
		ctx:             ctx,
		nowUnix:         float64(time.Now().Unix()),
		sequenceCounter: lastSeq,
		adSegments:      adSegments,
	}

	nowLocal := p.processStartTime
	for _, item := range schedules.Data {
		if p.isEndOfTimeline(p.processStartTime, p.globalRunningTime) {
			break
		}
		if err := processor.processScheduleItem(item, channel, nowLocal); err == ErrEndOfTimeline {
			break
		}
	}

	return p.flushRedisPipeline(processor.pipe)
}

func (p *Planner) flushRedisPipeline(pipe redis.Pipeliner) error {
	pipe.Expire(context.Background(), p.getRedisKey(), RedisExpiryHours*time.Hour)
	_, err := pipe.Exec(context.Background())
	return err
}

func (sp *SegmentProcessor) processScheduleItem(item models.ESPXScheduleItem, channel models.PlayoutChannel, nowLocal time.Time) error {
	if len(item.Programme.ProgrammeStreams) == 0 {
		return nil
	}
	streamID := item.Programme.ProgrammeStreams[0].StreamID
	if streamID == "" {
		return nil
	}

	startTime, err := utils.ParseESPXTime(item.LocalDateStart, channel.ContentTimezone)
	if err != nil {
		return err
	}
	if endTime, err := utils.ParseESPXTime(item.LocalDateEnd, channel.ContentTimezone); err == nil && endTime.Before(nowLocal) {
		return nil
	}

	epgStartUnix := float64(startTime.Unix())
	if epgStartUnix > sp.planner.globalRunningTime {
		sp.planner.globalRunningTime = epgStartUnix
	}

	// Khởi tạo mốc đếm thời gian ad đầu tiên nếu chưa có
	if sp.lastAdTime == 0 {
		sp.lastAdTime = sp.planner.globalRunningTime
	}

	return sp.processStreamSegments(streamID)
}

func (sp *SegmentProcessor) processStreamSegments(streamID string) error {
	pair := sp.planner.resolveRenditions(streamID)
	m3u8Path := fmt.Sprintf("%s/%s/%s/vod.m3u8", sp.planner.bucketName, streamID, pair.vod)
	m3u8Content, err := sp.planner.rclone.Cat(m3u8Path)
	if err != nil {
		return fmt.Errorf("failed to fetch m3u8: %w", err)
	}

	lines := strings.Split(m3u8Content, "\n")
	var currentDuration float64
	var ptsStart, ptsEnd int64
	firstSegmentInStream := true

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXTINF:"):
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			if dur, err := strconv.ParseFloat(durStr, 64); err == nil {
				currentDuration = dur
			}
		case strings.HasPrefix(line, "#EXT-X-PTS-RANGE:"):
			ptsRange := strings.TrimPrefix(line, "#EXT-X-PTS-RANGE:")
			parts := strings.Split(ptsRange, "-")
			if len(parts) == 2 {
				ptsStart, _ = strconv.ParseInt(parts[0], 10, 64)
				ptsEnd, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		case strings.HasSuffix(line, ".ts"):
			if sp.planner.isEndOfTimeline(sp.planner.processStartTime, sp.planner.globalRunningTime) {
				return ErrEndOfTimeline
			}

			// 1. Kiểm tra và chèn quảng cáo ngay trước segment video (nếu đến lịch)
			if err := sp.insertAdsIfNeeded(sp.planner.globalRunningTime); err != nil {
				return err
			}

			// Neo chuẩn PTS offset dựa trên globalRunningTime hiện tại tại thời điểm bắt đầu segment
			if firstSegmentInStream {
				targetPtsStart := sp.planner.globalRunningTime * 90000.0
				sp.currentPtsOffset = targetPtsStart - float64(ptsStart)
				firstSegmentInStream = false
			}

			adjustedPtsStart := float64(ptsStart) + sp.currentPtsOffset
			adjustedPtsEnd := float64(ptsEnd) + sp.currentPtsOffset

			mediaStart := adjustedPtsStart / 90000.0
			mediaEnd := adjustedPtsEnd / 90000.0
			calculatedDuration := mediaEnd - mediaStart
			if calculatedDuration <= 0 {
				calculatedDuration = currentDuration
			}

			// Nếu vừa chèn ad xong, globalRunningTime đã được đẩy lên, đồng bộ lại mediaStart cho video
			if sp.planner.globalRunningTime > mediaStart {
				mediaStart = sp.planner.globalRunningTime
				mediaEnd = mediaStart + calculatedDuration
			}

			// 2. Xử lý và ghi segment video thường vào Redis (chỉ gọi 1 hàm duy nhất quản lý sequence)
			if err := sp.processSegment(streamID, pair.vod, line, calculatedDuration, ptsStart, ptsEnd, mediaStart, mediaEnd); err != nil {
				return err
			}

			// Cập nhật globalRunningTime tiến lên theo segment video thực tế
			sp.planner.globalRunningTime = mediaEnd
		}
	}
	return nil
}

func (sp *SegmentProcessor) processSegment(streamID, vodDir, line string, duration float64, ptsStart, ptsEnd int64, mediaStart, mediaEnd float64) error {
	sp.sequenceCounter++ // Tăng sequence chuẩn +1 duy nhất tại đây cho video
	segmentPath := fmt.Sprintf("%s/%s/%s/%s", sp.planner.bucketName, streamID, vodDir, line)
	ptsOffset := sp.setTimelineBase(mediaStart)

	segment := models.SegmentItem{
		Sequence:    sp.sequenceCounter,
		StoragePath: segmentPath,
		Duration:    duration,
		StartTime:   int64(mediaStart),
		PtsStart:    ptsStart,
		PtsEnd:      ptsEnd,
		MediaStart:  mediaStart,
		MediaEnd:    mediaEnd,
		PtsOffset:   &ptsOffset,
	}

	if mediaStart >= (sp.nowUnix - WindowBufferSeconds) {
		if err := sp.planner.addSegmentToRedis(sp.pipe, sp.ctx, mediaStart, segment); err != nil {
			return err
		}
		sp.planner.totalSegmentsAdded++
	}

	return nil
}

// setTimelineBase ghim mốc timeline cục bộ vào segment đầu tiên và trả về
// PtsOffset (giây cục bộ) cho segment hiện tại: MediaStart - timelineBase.
func (sp *SegmentProcessor) setTimelineBase(mediaStart float64) float64 {
	if sp.timelineBase == 0 {
		sp.timelineBase = mediaStart
	}
	return mediaStart - sp.timelineBase
}

func (sp *SegmentProcessor) insertAdsIfNeeded(actualStartTime float64) error {
	if len(sp.adSegments) == 0 {
		return nil
	}

	// Kiểm tra xem đã đủ mốc 30 giây (AdInsertionInterval) kể từ lần chèn ad trước đó chưa
	if actualStartTime < sp.nowUnix || (actualStartTime-sp.lastAdTime) < float64(AdInsertionInterval) {
		return nil
	}

	// Tiến hành chèn cụm segment quảng cáo vào timeline
	for _, adSeg := range sp.adSegments {
		if sp.planner.isEndOfTimeline(sp.planner.processStartTime, sp.planner.globalRunningTime) {
			return ErrEndOfTimeline
		}

		sp.sequenceCounter++ // Tăng sequence tịnh tiến chuẩn +1
		adItem := adSeg
		adItem.Sequence = sp.sequenceCounter

		// Map ad nối tiếp chuẩn xác ngay sau globalRunningTime hiện tại
		adItem.MediaStart = sp.planner.globalRunningTime
		adItem.MediaEnd = sp.planner.globalRunningTime + adItem.Duration
		adItem.StartTime = int64(adItem.MediaStart)
		ptsOffset := sp.setTimelineBase(adItem.MediaStart)
		adItem.PtsOffset = &ptsOffset

		if err := sp.planner.addSegmentToRedis(sp.pipe, sp.ctx, adItem.MediaStart, adItem); err != nil {
			return err
		}

		sp.planner.globalRunningTime = adItem.MediaEnd
		sp.planner.totalSegmentsAdded++
		sp.totalAdsAdded++
	}

	// Cập nhật lại mốc thời gian của lần chèn ad gần nhất để sau 30 giây tiếp theo nó sẽ tiếp tục trigger
	sp.lastAdTime = sp.planner.globalRunningTime
	return nil
}

func (p *Planner) addSegmentToRedis(pipe redis.Pipeliner, ctx context.Context, runningTime float64, data models.SegmentItem) error {
	if p.isEndOfTimeline(p.processStartTime, runningTime) {
		return ErrEndOfTimeline
	}

	data.TimeText = utils.TimeNumToText(data.StartTime)
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	pipe.ZAdd(ctx, p.getRedisKey(), redis.Z{
		Score:  runningTime,
		Member: bytes,
	})

	if runningTime <= (float64(time.Now().Unix()) + 120) {
		_ = p.flushBatch(pipe, ctx)
	} else if p.totalSegmentsAdded > 0 && p.totalSegmentsAdded%WindowItemSize == 0 {
		_ = p.flushBatch(pipe, ctx)
	}
	return nil
}

func (p *Planner) isEndOfTimeline(processTime time.Time, runningTime float64) bool {
	endOfDay := utils.EndOfDay(processTime)
	return endOfDay.Before(time.Unix(int64(runningTime), 0))
}

func (p *Planner) flushBatch(pipe redis.Pipeliner, ctx context.Context) error {
	_, err := pipe.Exec(ctx)
	return err
}
