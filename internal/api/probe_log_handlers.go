package api

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tokhub/internal/store"
)

func (s *Server) adminProbeLogs(w http.ResponseWriter, r *http.Request) {
	query, err := probeLogQueryFromRequest(r, time.Now())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_probe_log_query", err.Error())
		return
	}
	result, err := s.repo.ProbeLogs(r.Context(), query)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "probe_logs_unavailable", "Could not load probe logs")
		return
	}
	for index := range result.Items {
		sanitizeProbeLogItem(&result.Items[index])
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminProbeLogDetail(w http.ResponseWriter, r *http.Request) {
	item, err := s.repo.ProbeLogDetail(r.Context(), chi.URLParam(r, "runID"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "probe_log_not_found", "Probe log not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "probe_log_unavailable", "Could not load probe log")
		return
	}
	sanitizeProbeLogItem(&item.ProbeLogItem)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) exportAdminProbeLogs(w http.ResponseWriter, r *http.Request) {
	query, err := probeLogExportQueryFromRequest(r, time.Now())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_probe_log_export_query", err.Error())
		return
	}
	rangeName := probeExportRangeName(r.URL.Query().Get("range"))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="tokhub-probe-logs-%s-%s.csv"`, rangeName, time.Now().Format("20060102")))

	writer := csv.NewWriter(w)
	started := false
	startCSV := func() error {
		if started {
			return nil
		}
		if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return err
		}
		started = true
		return writer.Write(probeLogCSVHeaders)
	}
	err = s.repo.ExportProbeLogs(r.Context(), query, func(item store.ProbeLogExportItem) error {
		if err := startCSV(); err != nil {
			return err
		}
		return writer.Write(probeLogCSVRecord(item))
	})
	if err != nil {
		if !started {
			w.Header().Del("Content-Disposition")
			writeError(w, r, http.StatusInternalServerError, "probe_log_export_failed", "Could not export probe logs")
		} else if s.logger != nil {
			s.logger.Error("probe log export interrupted", "error", err)
		}
		return
	}
	if err := startCSV(); err != nil {
		return
	}
	writer.Flush()
	if err := writer.Error(); err != nil && s.logger != nil {
		s.logger.Error("probe log export write failed", "error", err)
	}
}

var probeLogCSVHeaders = []string{
	"started_at", "finished_at", "channel_name", "provider", "model", "endpoint", "layer",
	"source", "source_zh", "status", "status_zh", "step", "step_zh", "http_status",
	"http_status_zh", "error_type", "error_type_zh", "latency_ms", "step_count",
	"upstream_error_summary", "run_id",
}

func probeLogCSVRecord(item store.ProbeLogExportItem) []string {
	finishedAt := ""
	if item.FinishedAt != nil {
		finishedAt = item.FinishedAt.Format(time.RFC3339)
	}
	httpStatus := ""
	if item.HTTPStatus != nil {
		httpStatus = strconv.Itoa(*item.HTTPStatus)
	}
	values := []string{
		item.StartedAt.Format(time.RFC3339), finishedAt, item.ChannelName, item.Provider, item.Model,
		probeExportEndpoint(item.Endpoint), item.Layer, item.Source, probeSourceMeaning(item.Source), item.Status,
		probeStatusMeaning(item.Status), item.Step, probeStepMeaning(item.Step), httpStatus,
		probeHTTPStatusMeaning(item.HTTPStatus, item.Step), item.ErrorType, probeErrorTypeMeaning(item.ErrorType),
		strconv.Itoa(item.LatencyMs), strconv.Itoa(item.StepCount), item.UpstreamErrorSummary, item.ID,
	}
	for index, value := range values {
		values[index] = probeCSVCell(value)
	}
	return values
}

func probeExportEndpoint(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func sanitizeProbeLogItem(item *store.ProbeLogItem) {
	item.Endpoint = probeExportEndpoint(item.Endpoint)
}

func probeExportRangeName(raw string) string {
	switch strings.TrimSpace(raw) {
	case "7d", "30d", "all":
		return strings.TrimSpace(raw)
	default:
		return "24h"
	}
}

func probeStatusMeaning(status string) string {
	return map[string]string{"ok": "成功", "warn": "警告", "auth_error": "认证失败", "down": "不可用", "na": "未执行", "running": "执行中", "failed": "失败/中断"}[status]
}

func probeStepMeaning(step string) string {
	return map[string]string{"parse_url": "解析地址", "dns": "DNS 解析", "tcp": "TCP 连接", "tls": "TLS 握手", "http": "HTTP 连通", "models": "模型列表", "generate": "真实生成", "probe": "探测准备"}[step]
}

func probeSourceMeaning(source string) string {
	return map[string]string{"scheduler": "定时任务", "manual": "手动探测", "admin_validate": "通道验证", "private_manual": "私有通道手动探测"}[source]
}

func probeHTTPStatusMeaning(status *int, step string) string {
	if status == nil {
		if step == "dns" || step == "tcp" || step == "tls" || step == "parse_url" {
			return "尚未进入 HTTP 阶段"
		}
		return "未收到 HTTP 响应"
	}
	if step == "http" && *status < 500 {
		switch *status {
		case 401:
			return "端点可达，HEAD 请求需要认证"
		case 403:
			return "端点可达，HEAD 请求被访问策略拒绝"
		case 404:
			return "端点可达，HEAD 路径未提供资源"
		}
	}
	if meaning := map[int]string{200: "请求成功", 201: "资源已创建", 204: "请求成功，无响应正文", 400: "请求格式或参数错误", 401: "认证失败，Key、来源 IP 或授权策略不通过", 403: "禁止访问，可能受权限、地区或 IP 白名单限制", 404: "接口路径或模型不存在", 408: "上游请求超时", 409: "请求发生冲突", 422: "请求内容无法处理", 429: "请求过于频繁或额度受限", 500: "上游内部错误", 502: "上游网关响应异常", 503: "上游暂时不可用", 504: "上游网关超时"}[*status]; meaning != "" {
		return meaning
	}
	switch {
	case *status >= 200 && *status < 300:
		return "请求成功"
	case *status >= 400 && *status < 500:
		return "客户端请求或授权被拒绝"
	case *status >= 500:
		return "上游服务异常"
	default:
		return "未收录状态码"
	}
}

func probeErrorTypeMeaning(errorType string) string {
	if errorType == "" {
		return ""
	}
	if meaning := map[string]string{"auth_error": "认证信息或来源授权未通过", "models_auth_error": "模型列表接口认证失败", "model_not_found": "目标模型未出现在模型列表中", "model_unavailable": "目标模型当前不可用", "models_timeout": "模型列表请求超时", "models_rate_limited": "模型列表请求被限流", "models_probe_skipped": "根据通道配置跳过模型列表探测", "l3_probe_skipped": "根据通道配置跳过真实生成探测", "slow_response": "响应耗时超过通道阈值", "rate_limited": "上游限流或额度不足", "empty_content": "生成成功但响应内容为空", "content_mismatch": "生成内容与探测预期不一致", "reasoning_only": "只返回推理内容，没有最终回答", "timeout": "请求超时", "dns_failed": "域名解析失败", "tcp_failed": "TCP 连接失败", "tls_failed": "TLS 握手或证书校验失败", "bad_endpoint": "通道端点格式错误", "probe_interrupted": "探测进程未正常完成或结果未成功写入"}[errorType]; meaning != "" {
		return meaning
	}
	return "未收录错误类型"
}

func probeLogQueryFromRequest(r *http.Request, now time.Time) (store.ProbeLogQuery, error) {
	return probeLogQueryFromRequestWithRange(r, now, false)
}

func probeLogExportQueryFromRequest(r *http.Request, now time.Time) (store.ProbeLogQuery, error) {
	return probeLogQueryFromRequestWithRange(r, now, true)
}

func probeLogQueryFromRequestWithRange(r *http.Request, now time.Time, allowAll bool) (store.ProbeLogQuery, error) {
	values := r.URL.Query()
	duration := 24 * time.Hour
	switch strings.TrimSpace(values.Get("range")) {
	case "", "24h":
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	case "all":
		if !allowAll {
			return store.ProbeLogQuery{}, fmt.Errorf("range must be 24h, 7d, or 30d")
		}
		duration = 0
	default:
		if allowAll {
			return store.ProbeLogQuery{}, fmt.Errorf("range must be 24h, 7d, 30d, or all")
		}
		return store.ProbeLogQuery{}, fmt.Errorf("range must be 24h, 7d, or 30d")
	}

	page, err := positiveQueryInt(values.Get("page"), 1)
	if err != nil {
		return store.ProbeLogQuery{}, fmt.Errorf("invalid page: %w", err)
	}
	if page > 100000 {
		return store.ProbeLogQuery{}, fmt.Errorf("page must not exceed 100000")
	}
	pageSize, err := positiveQueryInt(values.Get("pageSize"), 50)
	if err != nil || (pageSize != 25 && pageSize != 50 && pageSize != 100) {
		return store.ProbeLogQuery{}, fmt.Errorf("pageSize must be 25, 50, or 100")
	}
	httpStatus := 0
	if raw := strings.TrimSpace(values.Get("httpStatus")); raw != "" {
		httpStatus, err = strconv.Atoi(raw)
		if err != nil || httpStatus < 100 || httpStatus > 599 {
			return store.ProbeLogQuery{}, fmt.Errorf("httpStatus must be between 100 and 599")
		}
	}
	provider, err := boundedQueryValue(values.Get("provider"), 128, "provider")
	if err != nil {
		return store.ProbeLogQuery{}, err
	}
	channelID, err := boundedQueryValue(values.Get("channelId"), 200, "channelId")
	if err != nil {
		return store.ProbeLogQuery{}, err
	}
	errorType, err := boundedQueryValue(values.Get("errorType"), 128, "errorType")
	if err != nil {
		return store.ProbeLogQuery{}, err
	}
	source, err := boundedQueryValue(values.Get("source"), 64, "source")
	if err != nil {
		return store.ProbeLogQuery{}, err
	}
	layer, err := boundedQueryValue(strings.ToLower(values.Get("layer")), 16, "layer")
	if err != nil {
		return store.ProbeLogQuery{}, err
	}
	status, err := boundedQueryValue(strings.ToLower(values.Get("status")), 32, "status")
	if err != nil {
		return store.ProbeLogQuery{}, err
	}

	query := store.ProbeLogQuery{
		Provider:     provider,
		ChannelID:    channelID,
		Layer:        layer,
		Status:       status,
		HTTPStatus:   httpStatus,
		ErrorType:    errorType,
		Source:       source,
		OnlyAbnormal: strings.EqualFold(strings.TrimSpace(values.Get("onlyAbnormal")), "true"),
		Page:         page,
		PageSize:     pageSize,
	}
	if duration > 0 {
		query.From = now.Add(-duration)
		query.To = now
	}
	return query, nil
}

func probeCSVCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func boundedQueryValue(raw string, maxLength int, name string) (string, error) {
	value := strings.TrimSpace(raw)
	if len([]rune(value)) > maxLength {
		return "", fmt.Errorf("%s is too long", name)
	}
	return value, nil
}

func positiveQueryInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return value, nil
}
