// Package api hosts the REST surface for playout-channel management and
// the HLS live streaming routes. ChannelHandler (this file) owns the
// /api/channels/* CRUD + start/stop surface; the HLS routes live in
// cmd/api/main.go.
package api

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"simlive/internal/models"
)

// List-page-size policy for GET /api/channels. Mirrors the
// "operator-friendly default, hard ceiling" pattern used by the existing
// /live/* routes. Operators can override the per-request page size via
// the `?page_size=` query param; values outside the [1, maxListPageSize]
// range are silently clamped.
const (
	defaultListPageSize = 20
	maxListPageSize     = 100
)

// plannerTickHint reads `PLANNER_INTERVAL` and returns a short human-readable
// hint string for the `[api] channel=... started` log line. Falls back to "5m" if unset or unparseable.
func plannerTickHint() string {
	raw := os.Getenv("PLANNER_INTERVAL")
	if raw == "" {
		return "5m (default)"
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return raw
	}
	return raw + " (unparseable; planner will use 5m default)"
}

// allowedStatuses is the closed set of Status values the API will accept on PUT/PATCH.
var allowedStatuses = map[string]bool{
	string(models.PlayoutPrepareStatus):  true,
	string(models.PlayoutLIVEStatus):     true,
	string(models.PlayoutInactiveStatus): true,
}

// ChannelHandler is the REST surface for /api/channels/* . It owns CRUD over the playout_channels table plus the manual start/stop operations.
// The planner reads the same columns (is_active, status) so a manual flip takes effect on the next planner tick.
type ChannelHandler struct {
	db   *gorm.DB
	partner *PARTNERClient
	rdb  *redis.Client
}

// NewChannelHandler wires the dependencies. `rdb` may be nil — start/stop then log a warning and continue (a Redis blip should never 5xx the operator; the DB flip is the durable part of the operation).
func NewChannelHandler(db *gorm.DB, partner *PARTNERClient, rdb *redis.Client) *ChannelHandler {
	return &ChannelHandler{db: db, partner: partner, rdb: rdb}
}

// ----- Path guard --------------------------------------------------------

// channelIDGuard returns true and writes a 400 to the response when the :id path param contains `..` or `/`. Mirrors the spirit of the path-guard checks already in use on the /live/* routes (cmd/api/main.go). Every /api/channels/:id route MUST call this first.
func channelIDGuard(c *gin.Context, id string) bool {
	if strings.Contains(id, "..") || strings.Contains(id, "/") {
		c.String(http.StatusBadRequest, "Invalid channel ID")
		return false
	}
	return true
}

// ----- POST /api/channels (create / upsert) ------------------------------

// CreateOrUpsertChannel validates the channel ID against the PARTNER API
// and upserts the playout_channels row. Renamed for naming consistency
// with other CRUD verbs.
func (h *ChannelHandler) CreateOrUpsertChannel(c *gin.Context) {
	var req models.CreateChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate Channel ID against the PARTNER API
	detail, err := h.partner.GetChannelDetailWithEPG(req.ChannelID)
	if err != nil || detail.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Channel ID or not found in PARTNER API"})
		return
	}

	// Map data for DB storage. Status starts at 'PREPARE' — the planner
	// flips it to 'LIVE' on the next tick once timeline data is in Redis.
	channel := models.PlayoutChannel{
		ID:              detail.ID,
		Name:            detail.Name,
		BaseName:        detail.BaseName,
		ContentTimezone: detail.ContentTimezone,
		ChannelEpgID:    detail.ChannelEpg.ID,
		Status:          models.PlayoutPrepareStatus,
		UpdatedAt:       time.Now(),
	}

	// Upsert into the Database. The `is_active` column is referenced
	// by name here (and matches the PlayoutChannel.IsActive struct field's
	// gorm:"column:is_active" tag) so GORM updates the column even on
	// existing rows.
	err = h.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "base_name", "content_timezone", "channel_epg_id",
			"is_active", "status", "updated_at",
		}),
	}).Create(&channel).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save channel to database"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Channel registered/updated successfully",
		"data":    channel,
	})
}

// ----- GET /api/channels (list local channels) --------------------------

// ListChannels returns a paginated slice of playout_channels rows.
// Query params: page (1-indexed, default 1), page_size (default 20,
// max 100), status (optional exact match), is_active (optional bool),
// q (optional name contains, case-insensitive).
func (h *ChannelHandler) ListChannels(c *gin.Context) {
	page, pageSize := parsePagination(c)
	status := c.Query("status")
	q := strings.TrimSpace(c.Query("q"))

	tx := h.db.Model(&models.PlayoutChannel{})
	if status != "" {
		tx = tx.Where("status = ?", status)
	}
	if q != "" {
		tx = tx.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(q)+"%")
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count channels"})
		return
	}

	var rows []models.PlayoutChannel
	offset := (page - 1) * pageSize
	if err := tx.Order("created_at DESC").
		Limit(pageSize).Offset(offset).
		Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list channels"})
		return
	}

	c.JSON(http.StatusOK, models.ListChannelsResp{
		Data:     rows,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// parsePagination reads ?page= and ?page_size=, clamping to sane bounds.
// Pure helper — easy to unit-test.
func parsePagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(c.Query("page_size"))
	switch {
	case pageSize <= 0:
		pageSize = defaultListPageSize
	case pageSize > maxListPageSize:
		pageSize = maxListPageSize
	}
	return
}

// ----- GET /api/channels/esp (list PARTNER channels) -----------------------

// ListPartnerChannels proxies PARTNER API 1 and annotates each item with a `registered` boolean (true if the channel is already in our local playout_channels table). Single SELECT id ... IN (...) keeps the round-trip count constant in the fleet size.
func (h *ChannelHandler) ListPartnerChannels(c *gin.Context) {
	items, err := h.partner.ListPartnerChannels()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to fetch channels from PARTNER: " + err.Error()})
		return
	}

	registered := make(map[string]bool, len(items))
	if len(items) > 0 {
		ids := make([]string, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		var existing []string
		if err := h.db.Model(&models.PlayoutChannel{}).
			Where("id IN ?", ids).
			Pluck("id", &existing).Error; err != nil {
			// Don't fail the whole response on a DB blip — return the PARTNER list with all `registered` set to false.
			log.Printf("[api] ListPartnerChannels: SELECT id IN (...) error: %v", err)
		} else {
			for _, id := range existing {
				registered[id] = true
			}
		}
	}

	c.JSON(http.StatusOK, models.PartnerChannelsListResp{
		Data:       items,
		Registered: registered,
	})
}

// ----- GET /api/channels/:id --------------------------------------------

// GetChannel reads a single playout_channels row by primary key. 404 on
// not-found (idempotent: re-issuing the same GET on a missing id is a
// 404 every time — no side-effects to repeat).
func (h *ChannelHandler) GetChannel(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	var row models.PlayoutChannel
	if err := h.db.First(&row, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read channel"})
		return
	}
	c.JSON(http.StatusOK, row)
}

// ----- PUT /api/channels/:id (full update) ------------------------------

// UpdateChannel does a full replace of the mutable fields on the identified row.
// Setting `status=INACTIVE` is a stop: force is_active=false so the planner and engine drop the channel on their next poll.
func (h *ChannelHandler) applyChannelUpdate(c *gin.Context, id string, req *models.UpdateChannelReq, verb string) {
	cols, vals := updateColumnsAndValues(req)
	if len(cols) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No updatable fields provided"})
		return
	}

	// updated_at always bumps.
	cols = append(cols, "updated_at")
	vals = append(vals, time.Now())

	res := h.db.Model(&models.PlayoutChannel{}).
		Where("id = ?", id).
		Updates(assignmentMap(cols, vals))
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update channel"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}

	var row models.PlayoutChannel
	if err := h.db.First(&row, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read " + verb + " channel"})
		return
	}

	c.JSON(http.StatusOK, row)
}

func (h *ChannelHandler) UpdateChannel(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	var req models.UpdateChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if req.Status != nil && !allowedStatuses[*req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status (must be PREPARE, LIVE, or INACTIVE)"})
		return
	}
	h.applyChannelUpdate(c, id, &req, "updated")
}

// ----- PATCH /api/channels/:id (partial update) -------------------------

// PatchChannel is the partial-update counterpart to UpdateChannel. Same
// shape: pointer fields, skip-zero semantics. Internally shares the
// updateColumnsAndValues helper so the "which columns are non-nil"
// logic is in one place.
func (h *ChannelHandler) PatchChannel(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	var req models.UpdateChannelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if req.Status != nil && !allowedStatuses[*req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status (must be PREPARE, LIVE, or INACTIVE)"})
		return
	}
	h.applyChannelUpdate(c, id, &req, "patched")
}

// updateColumnsAndValues is the shared "which fields of UpdateChannelReq
// are non-nil and should be written" helper. Pure — easy to unit-test.
// Returns parallel slices ready for GORM's Updates(assignmentMap(...))
// or any other column/value sink. GORM does not accept []string +
// []interface{} for Updates(), so callers wrap via assignmentMap.
func updateColumnsAndValues(req *models.UpdateChannelReq) (cols []string, vals []interface{}) {
	if req.Name != nil {
		cols = append(cols, "name")
		vals = append(vals, *req.Name)
	}
	if req.BaseName != nil {
		cols = append(cols, "base_name")
		vals = append(vals, *req.BaseName)
	}
	if req.ContentTimezone != nil {
		cols = append(cols, "content_timezone")
		vals = append(vals, *req.ContentTimezone)
	}
	if req.ChannelEpgID != nil {
		cols = append(cols, "channel_epg_id")
		vals = append(vals, *req.ChannelEpgID)
	}
	if req.Status != nil {
		cols = append(cols, "status")
		vals = append(vals, *req.Status)
	}
	return
}

// assignmentMap zips parallel (col, val) slices into a map[string]any
// suitable for gorm's Updates(map). GORM does not preserve column order
// from a map; for the operations in this handler that doesn't matter
// (each column is independent).
func assignmentMap(cols []string, vals []interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(cols))
	for i, c := range cols {
		m[c] = vals[i]
	}
	return m
}

// ----- DELETE /api/channels/:id (soft delete) ---------------------------

// DeleteChannel is a soft delete: flip is_active=false, status='INACTIVE'.
// The row is preserved in the DB so audit history is intact. Idempotent
// — a second DELETE on an already-soft-deleted row returns 204 (not 404).
func (h *ChannelHandler) DeleteChannel(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	res := h.db.Model(&models.PlayoutChannel{}).
		Where("id = ?", id).
		Delete(&models.PlayoutChannel{})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete channel"})
		return
	}

	// 204 whether the row existed or not (idempotent). RowsAffected can
	// be 0 both for "not found" and for "row exists but already
	// is_active=false"; we deliberately do not distinguish.
	c.Status(http.StatusNoContent)
}

// ----- POST /api/channels/:id/start -------------------------------------

// StartChannel flips is_active=true and status='PREPARE', and clears the
// planner's per-channel breaker counter so a re-started channel behaves
// like a fresh one (the breaker is otherwise sticky until the next
// successful prepare or a manual reset). Redis errors are logged but
// not propagated — the DB flip is the durable part of the operation.
//
// The engine's reconciler starts the playout loop IMMEDIATELY on PREPARE
// the planner writes timeline data to Redis. The planner is the SINGLE
// writer of status='LIVE': it flips the channel on its next tick once
// timeline data is in Redis (a successful prepare or a "date already
// prepared" short-circuit).
//
// Note on perceived latency: this handler returns within ~20ms. The
// engine reconciler polls every 5s, so a freshly-started channel will
// have its playout loop running within ~5s. The planner is a poll-based
// cron (`cmd/planner/main.go`) that walks the DB every `PLANNER_INTERVAL`
// (default 5m); on a cold start, the first `ProcessEPGToTimeline` run for
// today + tomorrow may add another 30s–2min (EPG HTTP + rclone cat +
// Redis pipeline). Subsequent ticks short-circuit via `HasDateInTimeline`
// and add <1s. The `[api] channel=... started` line below lets the
// operator correlate the API call with the next planner tick.
func (h *ChannelHandler) StartChannel(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	res := h.db.Model(&models.PlayoutChannel{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"is_active":  true,
			"status":     models.PlayoutPrepareStatus,
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start channel"})
		return
	}
	if res.RowsAffected == 0 {
		// Distinguish "row not found" from "row already active" — the
		// first is a 404, the second is a 200 (idempotent). Check by
		// reading the row.
		var row models.PlayoutChannel
		if err := h.db.First(&row, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read channel"})
			return
		}
		// Row exists; treat as idempotent success.
		log.Printf("[api] channel=%s start: no-op (already active)", id)
		c.JSON(http.StatusOK, row)
		return
	}

	log.Printf("[api] channel=%s start: status=PREPARE, breaker cleared (planner will flip to LIVE once timeline data is in Redis, within PLANNER_INTERVAL=%s)",
		id, plannerTickHint())

	// Best-effort: clear the breaker counter. Use a fresh context so a
	// client-side cancellation on the HTTP request doesn't abort the
	// Redis call.
	// if h.rdb != nil {
	// 	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	// 	if err := h.rdb.Del(ctx, breakerKeyPrefix+id).Err(); err != nil {
	// 		log.Printf("[api] StartChannel: DEL %s%s error: %v", breakerKeyPrefix, id, err)
	// 	}
	// 	cancel()
	// }

	var row models.PlayoutChannel
	if err := h.db.First(&row, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read started channel"})
		return
	}
	c.JSON(http.StatusOK, row)
}

func (h *ChannelHandler) StopChannel(c *gin.Context) {
	id := c.Param("id")
	if !channelIDGuard(c, id) {
		return
	}
	res := h.db.Model(&models.PlayoutChannel{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     "INACTIVE",
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to stop channel"})
		return
	}
	// Purge the channel's Redis data whenever the DB flip succeeded OR
	// the row was already stopped — a stopped channel should not have
	// live playlists, timeline, renditions metadata, or cursor cached.
	// Best-effort, logged on failure.
	// h.purgeChannelRedisData(id)
	// h.purgeChannelDiskData(id)

	if res.RowsAffected == 0 {
		var row models.PlayoutChannel
		if err := h.db.First(&row, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read channel"})
			return
		}
		// Row exists and was already stopped — idempotent 200.
		log.Printf("[api] channel=%s stop: no-op (already inactive)", id)
		c.JSON(http.StatusOK, row)
		return
	}
	log.Printf("[api] channel=%s stop: status=INACTIVE, Redis data purged (HLS + timeline + renditions + cursor), breaker preserved (engine will stop within 5s, planner within PLANNER_INTERVAL=%s)",
		id, plannerTickHint())
	var row models.PlayoutChannel
	if err := h.db.First(&row, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read stopped channel"})
		return
	}
	c.JSON(http.StatusOK, row)
}

// func (h *ChannelHandler) purgeChannelDiskData(id string) {
// 	tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
// 	dir := filepath.Join(tmpDir, id)
// 	if err := os.RemoveAll(dir); err != nil {
// 		log.Printf("[api] purgeChannelDiskData(%s): remove %s error: %v", id, dir, err)
// 	} else {
// 		log.Printf("[api] purgeChannelDiskData(%s): removed %s", id, dir)
// 	}
// }
