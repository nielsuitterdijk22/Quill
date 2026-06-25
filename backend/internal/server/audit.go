package server

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// logAudit writes a best-effort audit log entry. It never fails the calling
// handler — errors are logged at warn level and swallowed.
func (s *Server) logAudit(r *http.Request, action, targetType, targetID string, metadata map[string]any) {
	id, _ := identityFrom(r.Context())

	raw, err := json.Marshal(metadata)
	if err != nil {
		raw = []byte("{}")
	}

	actorID := uuid.NullUUID{}
	if id.UserID != (uuid.UUID{}) {
		actorID = uuid.NullUUID{UUID: id.UserID, Valid: true}
	}

	_, err = s.store.InsertAuditLog(r.Context(), db.InsertAuditLogParams{
		ActorUserID:   actorID,
		Action:        action,
		TargetType:    targetType,
		TargetID:      targetID,
		Metadata:      raw,
		IPAddress:     r.RemoteAddr,
		ActorUsername: id.Username,
	})
	if err != nil {
		s.logger.Warn("audit log write failed", "action", action, "error", err)
	}
}

// auditLogResponse is the public JSON shape for an audit log entry.
type auditLogResponse struct {
	ID            int64          `json:"id"`
	Action        string         `json:"action"`
	TargetType    string         `json:"targetType"`
	TargetID      string         `json:"targetId"`
	Metadata      map[string]any `json:"metadata"`
	ActorUsername string         `json:"actorUsername"`
	IPAddress     string         `json:"ipAddress"`
	CreatedAt     time.Time      `json:"createdAt"`
}

func newAuditLogResponse(e db.AuditLog) auditLogResponse {
	var meta map[string]any
	if len(e.Metadata) > 0 {
		_ = json.Unmarshal(e.Metadata, &meta)
	}
	if meta == nil {
		meta = map[string]any{}
	}
	return auditLogResponse{
		ID:            e.ID,
		Action:        e.Action,
		TargetType:    e.TargetType,
		TargetID:      e.TargetID,
		Metadata:      meta,
		ActorUsername: e.ActorUsername,
		IPAddress:     e.IPAddress,
		CreatedAt:     e.CreatedAt,
	}
}

// handleListAuditLog returns audit log entries with optional filtering.
// Admin only — gate is enforced by the requireAdmin middleware on this route.
func (s *Server) handleListAuditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	actionPrefix := q.Get("action")
	since, until := parseAuditDateRange(q.Get("since"), q.Get("until"))

	limit := int32(100)
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = int32(l)
	}
	offset := int32(0)
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = int32(o)
	}

	params := db.ListAuditLogFilteredParams{
		ActionPrefix: actionPrefix,
		Since:        since,
		Until:        until,
		Limit:        limit,
		Offset:       offset,
	}
	entries, err := s.store.ListAuditLogFiltered(r.Context(), params)
	if err != nil {
		s.logger.Error("list audit log failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not load audit log")
		return
	}

	total, err := s.store.CountAuditLogFiltered(r.Context(), db.CountAuditLogFilteredParams{
		ActionPrefix: actionPrefix,
		Since:        since,
		Until:        until,
	})
	if err != nil {
		total = 0
	}

	out := make([]auditLogResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, newAuditLogResponse(e))
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"entries": out,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// handleExportAuditLog streams the filtered audit log as a CSV download.
// Admin only.
func (s *Server) handleExportAuditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	actionPrefix := q.Get("action")
	since, until := parseAuditDateRange(q.Get("since"), q.Get("until"))

	// Fetch up to 50 000 rows for export.
	entries, err := s.store.ListAuditLogFiltered(r.Context(), db.ListAuditLogFilteredParams{
		ActionPrefix: actionPrefix,
		Since:        since,
		Until:        until,
		Limit:        50000,
		Offset:       0,
	})
	if err != nil {
		s.logger.Error("export audit log failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not export audit log")
		return
	}

	// Log the export itself before streaming.
	s.logAudit(r, "admin.audit_log_exported", "audit_log", "", map[string]any{
		"rows":          len(entries),
		"action_filter": actionPrefix,
	})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.csv"`)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "created_at", "actor_username", "ip_address", "action", "target_type", "target_id", "metadata"})
	for _, e := range entries {
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10),
			e.CreatedAt.UTC().Format(time.RFC3339),
			e.ActorUsername,
			e.IPAddress,
			e.Action,
			e.TargetType,
			e.TargetID,
			string(e.Metadata),
		})
	}
	cw.Flush()
}

// parseAuditDateRange parses optional since/until query params into
// pgtype.Timestamptz values (null when absent or unparseable). Accepts both
// RFC3339 and date-only YYYY-MM-DD; date-only since = start of day UTC,
// date-only until = end of day UTC.
func parseAuditDateRange(sinceStr, untilStr string) (pgtype.Timestamptz, pgtype.Timestamptz) {
	var since, until pgtype.Timestamptz
	if s := strings.TrimSpace(sinceStr); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = pgtype.Timestamptz{Time: t, Valid: true}
		} else if t, err := time.Parse("2006-01-02", s); err == nil {
			since = pgtype.Timestamptz{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
		}
	}
	if u := strings.TrimSpace(untilStr); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			until = pgtype.Timestamptz{Time: t, Valid: true}
		} else if t, err := time.Parse("2006-01-02", u); err == nil {
			until = pgtype.Timestamptz{Time: time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC), Valid: true}
		}
	}
	return since, until
}
