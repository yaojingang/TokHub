package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/store"
)

func (s *Server) publicOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.repo.PublicOverview(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "overview_unavailable", "Could not load public overview")
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *Server) publicChannels(w http.ResponseWriter, r *http.Request) {
	list, err := s.repo.PublicChannels(r.Context(), publicChannelFilter(r))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channels_unavailable", "Could not load public channels")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) publicChannelsExport(w http.ResponseWriter, r *http.Request) {
	filter := publicChannelFilter(r)
	filter.Page = 1
	filter.PageSize = 100
	var rows []store.PublicChannel
	for {
		list, err := s.repo.PublicChannels(r.Context(), filter)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "channels_export_unavailable", "Could not export public channels")
			return
		}
		rows = append(rows, list.Items...)
		if len(rows) >= list.Total || len(list.Items) == 0 || len(rows) >= 5000 {
			break
		}
		filter.Page++
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tokhub-public-channels.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"id", "public_slug", "name", "provider", "type", "model", "upstream_model", "status", "status_label",
		"score", "uptime_24h", "success_rate", "latency_p95_ms", "l1_status", "l2_status", "l3_status",
		"tokens_used", "cost_usd", "input_per_mtok", "output_per_mtok", "last_probe_at", "endpoint", "official_site_url",
	})
	for _, ch := range rows {
		_ = writer.Write([]string{
			ch.ID,
			ch.PublicSlug,
			ch.Name,
			ch.Provider,
			ch.Type,
			ch.Model,
			ch.UpstreamModel,
			ch.Status,
			ch.StatusLabel,
			strconv.Itoa(ch.Score),
			strconv.FormatFloat(ch.Uptime24h, 'f', 2, 64),
			strconv.FormatFloat(ch.SuccessRate, 'f', 2, 64),
			strconv.Itoa(ch.LatencyP95Ms),
			ch.L1Status,
			ch.L2Status,
			ch.L3Status,
			strconv.Itoa(ch.TokensUsed),
			strconv.FormatFloat(ch.CostUSD, 'f', 6, 64),
			strconv.FormatFloat(ch.InputPerMTok, 'f', 4, 64),
			strconv.FormatFloat(ch.OutputPerMTok, 'f', 4, 64),
			ch.LastProbeAt.Format(time.RFC3339),
			ch.Endpoint,
			ch.OfficialSiteURL,
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		s.logger.Warn("public channels csv export failed", "error", err)
	}
}

func publicChannelFilter(r *http.Request) store.ChannelFilter {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	return store.ChannelFilter{
		Provider: r.URL.Query().Get("provider"),
		Status:   r.URL.Query().Get("status"),
		Query:    r.URL.Query().Get("query"),
		Page:     page,
		PageSize: pageSize,
	}
}

func (s *Server) publicChannel(w http.ResponseWriter, r *http.Request) {
	channelID := chi.URLParam(r, "channelID")
	detail, err := s.repo.PublicChannel(r.Context(), channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "channel_not_found", "Channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "channel_unavailable", "Could not load channel")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) publicChannelSeries(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	series, err := s.repo.PublicChannelSeries(r.Context(), chi.URLParam(r, "channelID"), days)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "channel_not_found", "Channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "series_unavailable", "Could not load channel series")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": series})
}

func (s *Server) publicProviderRank(w http.ResponseWriter, r *http.Request) {
	rank, err := s.repo.PublicProviderRankForRange(r.Context(), publicRangeDays(r))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "rank_unavailable", "Could not load provider rank")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rank})
}

func (s *Server) publicErrorsSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.repo.PublicErrorsSummaryForRange(r.Context(), publicRangeDays(r))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "errors_unavailable", "Could not load error summary")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": summary})
}

func publicRangeDays(r *http.Request) int {
	value := strings.TrimSpace(r.URL.Query().Get("range"))
	if value == "" {
		value = strings.TrimSpace(r.URL.Query().Get("days"))
	}
	switch value {
	case "24", "1":
		return 1
	case "7":
		return 7
	case "30":
		return 30
	case "all":
		return 0
	default:
		days, _ := strconv.Atoi(value)
		if days < 0 {
			return 0
		}
		if days > 90 {
			return 90
		}
		return days
	}
}

func (s *Server) statusStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "stream_unavailable", "Streaming unsupported")
		return
	}

	send := func() bool {
		overview, err := s.repo.PublicOverview(r.Context())
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", `{"message":"overview unavailable"}`)
			flusher.Flush()
			return false
		}
		raw, _ := json.Marshal(overview)
		fmt.Fprintf(w, "event: status\ndata: %s\n\n", raw)
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}
