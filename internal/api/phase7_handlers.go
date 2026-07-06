package api

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"tokhub/internal/store"
)

func (s *Server) adminAlertCenter(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	center, err := s.repo.AlertCenter(r.Context(), "admin", "", user)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "alerts_unavailable", "Could not load alerts")
		return
	}
	s.backfillNotificationChannelTargets()
	writeJSON(w, http.StatusOK, center)
}

func (s *Server) consoleAlertCenter(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspace(w, r)
	if !ok {
		return
	}
	center, err := s.repo.AlertCenter(r.Context(), "console", orgID, user)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "alerts_unavailable", "Could not load workspace alerts")
		return
	}
	s.backfillNotificationChannelTargets()
	writeJSON(w, http.StatusOK, center)
}

func (s *Server) createAdminAlertRule(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var input store.AlertRuleInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	rule, err := s.repo.UpsertAlertRule(r.Context(), "admin", "", user.ID, input)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "alert_rule_invalid", "Could not create alert rule")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rule": rule})
}

func (s *Server) createConsoleAlertRule(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	var input store.AlertRuleInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	rule, err := s.repo.UpsertAlertRule(r.Context(), "console", orgID, user.ID, input)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "alert_rule_invalid", "Could not create alert rule")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rule": rule})
}

func (s *Server) patchAdminAlertRule(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	var input store.AlertRuleInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	rule, err := s.repo.PatchAlertRule(r.Context(), "admin", "", chi.URLParam(r, "ruleID"), user.ID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "alert_rule_not_found", "Alert rule not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "alert_rule_invalid", "Could not update alert rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) patchConsoleAlertRule(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	var input store.AlertRuleInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	rule, err := s.repo.PatchAlertRule(r.Context(), "console", orgID, chi.URLParam(r, "ruleID"), user.ID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "alert_rule_not_found", "Alert rule not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "alert_rule_invalid", "Could not update alert rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": rule})
}

func (s *Server) deleteAdminAlertRule(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	err := s.repo.DeleteAlertRule(r.Context(), "admin", "", chi.URLParam(r, "ruleID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "alert_rule_not_found", "Alert rule not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "alert_rule_delete_failed", "Could not delete alert rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) deleteConsoleAlertRule(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	err := s.repo.DeleteAlertRule(r.Context(), "console", orgID, chi.URLParam(r, "ruleID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "alert_rule_not_found", "Alert rule not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "alert_rule_delete_failed", "Could not delete alert rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) bulkAdminAlertRules(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	s.bulkAlertRules(w, r, "admin", "", user.ID)
}

func (s *Server) bulkConsoleAlertRules(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	s.bulkAlertRules(w, r, "console", orgID, user.ID)
}

func (s *Server) bulkAlertRules(w http.ResponseWriter, r *http.Request, scope string, orgID string, actorID string) {
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one alert rule")
		return
	}
	var rules []store.AlertRule
	var err error
	switch req.Action {
	case "enable":
		rules, err = s.repo.BulkUpdateAlertRules(r.Context(), scope, orgID, req.IDs, actorID, true)
	case "disable":
		rules, err = s.repo.BulkUpdateAlertRules(r.Context(), scope, orgID, req.IDs, actorID, false)
	case "enabled":
		if req.Enabled == nil {
			writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Missing enabled flag")
			return
		}
		rules, err = s.repo.BulkUpdateAlertRules(r.Context(), scope, orgID, req.IDs, actorID, *req.Enabled)
	case "delete":
		rules, err = s.repo.BulkDeleteAlertRules(r.Context(), scope, orgID, req.IDs, actorID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "alert_rule_not_found", "One or more alert rules were not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "alert_rule_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules})
}

func (s *Server) createAdminNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	input, ok := s.notificationChannelInput(w, r, true)
	if !ok {
		return
	}
	channel, err := s.repo.UpsertNotificationChannel(r.Context(), "admin", "", user.ID, input)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "notification_invalid", "Could not create notification channel")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": channel})
}

func (s *Server) patchAdminNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	input, ok := s.notificationChannelInput(w, r, false)
	if !ok {
		return
	}
	s.backfillNotificationChannelTargets()
	channel, err := s.repo.PatchNotificationChannel(r.Context(), "admin", "", chi.URLParam(r, "channelID"), user.ID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "notification_not_found", "Notification channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "notification_invalid", "Could not update notification channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel})
}

func (s *Server) createConsoleNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	input, inputOK := s.notificationChannelInput(w, r, true)
	if !inputOK {
		return
	}
	channel, err := s.repo.UpsertNotificationChannel(r.Context(), "console", orgID, user.ID, input)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "notification_invalid", "Could not create notification channel")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": channel})
}

func (s *Server) patchConsoleNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	input, inputOK := s.notificationChannelInput(w, r, false)
	if !inputOK {
		return
	}
	s.backfillNotificationChannelTargets()
	channel, err := s.repo.PatchNotificationChannel(r.Context(), "console", orgID, chi.URLParam(r, "channelID"), user.ID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "notification_not_found", "Notification channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "notification_invalid", "Could not update notification channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": channel})
}

func (s *Server) testAdminNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	delivery, err := s.repo.TestNotificationChannel(r.Context(), "admin", "", chi.URLParam(r, "channelID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "notification_not_found", "Notification channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "notification_test_failed", "Could not test notification channel")
		return
	}
	s.backfillNotificationChannelTargets()
	delivery = s.deliverAlertNotification(r.Context(), delivery)
	writeJSON(w, http.StatusOK, map[string]any{"delivery": delivery})
}

func (s *Server) deleteAdminNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	err := s.repo.DeleteNotificationChannel(r.Context(), "admin", "", chi.URLParam(r, "channelID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "notification_not_found", "Notification channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "notification_delete_failed", "Could not delete notification channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) deleteConsoleNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	err := s.repo.DeleteNotificationChannel(r.Context(), "console", orgID, chi.URLParam(r, "channelID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "notification_not_found", "Notification channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "notification_delete_failed", "Could not delete notification channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) bulkAdminNotificationChannels(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	s.bulkNotificationChannels(w, r, "admin", "", user.ID)
}

func (s *Server) bulkConsoleNotificationChannels(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	s.bulkNotificationChannels(w, r, "console", orgID, user.ID)
}

func (s *Server) bulkNotificationChannels(w http.ResponseWriter, r *http.Request, scope string, orgID string, actorID string) {
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one notification channel")
		return
	}
	var channels []store.NotificationChannel
	var err error
	switch req.Action {
	case "enable":
		channels, err = s.repo.BulkUpdateNotificationChannels(r.Context(), scope, orgID, req.IDs, actorID, true)
	case "disable":
		channels, err = s.repo.BulkUpdateNotificationChannels(r.Context(), scope, orgID, req.IDs, actorID, false)
	case "enabled":
		if req.Enabled == nil {
			writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Missing enabled flag")
			return
		}
		channels, err = s.repo.BulkUpdateNotificationChannels(r.Context(), scope, orgID, req.IDs, actorID, *req.Enabled)
	case "delete":
		channels, err = s.repo.BulkDeleteNotificationChannels(r.Context(), scope, orgID, req.IDs, actorID)
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_bulk_action", "Unsupported bulk action")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "notification_not_found", "One or more notification channels were not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "notification_bulk_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": channels})
}

func (s *Server) notificationChannelInput(w http.ResponseWriter, r *http.Request, create bool) (store.NotificationChannelInput, bool) {
	var input store.NotificationChannelInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return store.NotificationChannelInput{}, false
	}
	channelType := strings.TrimSpace(input.Type)
	if channelType != "webhook" && channelType != "feishu" {
		channelType = "email"
	}
	input.Type = channelType
	target := strings.TrimSpace(input.Target)
	if create && target == "" {
		target = "admin@tokhub.local"
	}
	if target == "" {
		return input, true
	}
	if s.secretBox == nil {
		writeError(w, r, http.StatusInternalServerError, "encryption_unavailable", "Notification target encryption is unavailable")
		return store.NotificationChannelInput{}, false
	}
	encrypted, err := s.secretBox.Encrypt(target)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "encryption_failed", "Could not encrypt notification target")
		return store.NotificationChannelInput{}, false
	}
	input.Target = target
	input.TargetCiphertext = encrypted.Ciphertext
	input.TargetNonce = encrypted.Nonce
	input.TargetMask = store.MaskNotificationTarget(channelType, target)
	input.TargetFingerprint = encrypted.Fingerprint
	return input, true
}

func (s *Server) testConsoleNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	delivery, err := s.repo.TestNotificationChannel(r.Context(), "console", orgID, chi.URLParam(r, "channelID"), user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "notification_not_found", "Notification channel not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "notification_test_failed", "Could not test notification channel")
		return
	}
	s.backfillNotificationChannelTargets()
	delivery = s.deliverAlertNotification(r.Context(), delivery)
	writeJSON(w, http.StatusOK, map[string]any{"delivery": delivery})
}

func (s *Server) evaluateAdminAlerts(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	deliveries, err := s.repo.EvaluateAlerts(r.Context(), "admin", "", user)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "alert_evaluation_failed", "Could not evaluate alerts")
		return
	}
	s.backfillNotificationChannelTargets()
	deliveries = s.deliverAlertNotifications(r.Context(), deliveries)
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": deliveries})
}

func (s *Server) evaluateConsoleAlerts(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	deliveries, err := s.repo.EvaluateAlerts(r.Context(), "console", orgID, user)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "alert_evaluation_failed", "Could not evaluate workspace alerts")
		return
	}
	s.backfillNotificationChannelTargets()
	deliveries = s.deliverAlertNotifications(r.Context(), deliveries)
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": deliveries})
}

func (s *Server) adminAuditLogs(w http.ResponseWriter, r *http.Request) {
	result, err := s.repo.AuditLogs(r.Context(), auditQueryFromRequest(r, ""))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "audit_unavailable", "Could not load audit logs")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) consoleAuditLogs(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := s.consoleWorkspace(w, r)
	if !ok {
		return
	}
	result, err := s.repo.AuditLogs(r.Context(), auditQueryFromRequest(r, orgID))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "audit_unavailable", "Could not load workspace audit logs")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminAuditDetail(w http.ResponseWriter, r *http.Request) {
	item, err := s.repo.AuditLogDetail(r.Context(), chi.URLParam(r, "auditID"), "")
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "audit_not_found", "Audit log not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "audit_unavailable", "Could not load audit log")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) consoleAuditDetail(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := s.consoleWorkspace(w, r)
	if !ok {
		return
	}
	item, err := s.repo.AuditLogDetail(r.Context(), chi.URLParam(r, "auditID"), orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "audit_not_found", "Audit log not found")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "audit_unavailable", "Could not load audit log")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) exportAdminAudit(w http.ResponseWriter, r *http.Request) {
	s.exportAudit(w, r, "")
}

func (s *Server) exportConsoleAudit(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := s.consoleWorkspace(w, r)
	if !ok {
		return
	}
	s.exportAudit(w, r, orgID)
}

func (s *Server) adminIncidents(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.IncidentsWithFilter(r.Context(), incidentQueryFromRequest(r, ""))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "incidents_unavailable", "Could not load incidents")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) consoleIncidents(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := s.consoleWorkspace(w, r)
	if !ok {
		return
	}
	items, err := s.repo.IncidentsWithFilter(r.Context(), incidentQueryFromRequest(r, orgID))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "incidents_unavailable", "Could not load workspace incidents")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createAdminIncident(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	s.createIncident(w, r, "", user)
}

func (s *Server) createConsoleIncident(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	s.createIncident(w, r, orgID, user)
}

func (s *Server) createIncident(w http.ResponseWriter, r *http.Request, orgID string, user store.PublicUser) {
	var input store.IncidentInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	item, err := s.repo.CreateIncident(r.Context(), orgID, user, input)
	if err != nil {
		writeIncidentMutationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) bulkAdminIncidents(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	s.bulkIncidents(w, r, "", user)
}

func (s *Server) bulkConsoleIncidents(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	s.bulkIncidents(w, r, orgID, user)
}

func (s *Server) bulkIncidents(w http.ResponseWriter, r *http.Request, orgID string, user store.PublicUser) {
	var req adminBulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	if !hasBulkSelection(req.IDs) {
		writeError(w, r, http.StatusBadRequest, "empty_bulk_selection", "Select at least one incident")
		return
	}
	items, err := s.repo.BulkUpdateIncidents(r.Context(), orgID, user, req.Action, req.IDs, strings.TrimSpace(req.Message))
	if err != nil {
		writeIncidentMutationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) patchAdminIncident(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	s.patchIncident(w, r, "", user)
}

func (s *Server) patchConsoleIncident(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	s.patchIncident(w, r, orgID, user)
}

func (s *Server) patchIncident(w http.ResponseWriter, r *http.Request, orgID string, user store.PublicUser) {
	var input store.IncidentInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return
	}
	item, err := s.repo.UpdateIncident(r.Context(), orgID, user, chi.URLParam(r, "incidentID"), input)
	if err != nil {
		writeIncidentMutationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) resolveAdminIncident(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	s.resolveIncident(w, r, "", user)
}

func (s *Server) resolveConsoleIncident(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	s.resolveIncident(w, r, orgID, user)
}

func (s *Server) resolveIncident(w http.ResponseWriter, r *http.Request, orgID string, user store.PublicUser) {
	message, ok := incidentActionMessage(w, r)
	if !ok {
		return
	}
	item, err := s.repo.ResolveIncident(r.Context(), orgID, user, chi.URLParam(r, "incidentID"), message)
	if err != nil {
		writeIncidentMutationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) reopenAdminIncident(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	s.reopenIncident(w, r, "", user)
}

func (s *Server) reopenConsoleIncident(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	s.reopenIncident(w, r, orgID, user)
}

func (s *Server) reopenIncident(w http.ResponseWriter, r *http.Request, orgID string, user store.PublicUser) {
	message, ok := incidentActionMessage(w, r)
	if !ok {
		return
	}
	item, err := s.repo.ReopenIncident(r.Context(), orgID, user, chi.URLParam(r, "incidentID"), message)
	if err != nil {
		writeIncidentMutationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteAdminIncident(w http.ResponseWriter, r *http.Request) {
	user, _ := s.userFromRequest(r)
	s.deleteIncident(w, r, "", user)
}

func (s *Server) deleteConsoleIncident(w http.ResponseWriter, r *http.Request) {
	user, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	s.deleteIncident(w, r, orgID, user)
}

func (s *Server) deleteIncident(w http.ResponseWriter, r *http.Request, orgID string, user store.PublicUser) {
	message, ok := incidentActionMessage(w, r)
	if !ok {
		return
	}
	if err := s.repo.DeleteIncident(r.Context(), orgID, user, chi.URLParam(r, "incidentID"), message); err != nil {
		writeIncidentMutationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) adminGovernanceSummary(w http.ResponseWriter, r *http.Request) {
	_ = s.repo.RecomputeUsageDailyRollups(r.Context(), "")
	summary, err := s.repo.GovernanceSummary(r.Context(), "admin", "")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "governance_unavailable", "Could not load governance summary")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) consoleGovernanceSummary(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := s.consoleWorkspace(w, r)
	if !ok {
		return
	}
	_ = s.repo.RecomputeUsageDailyRollups(r.Context(), orgID)
	summary, err := s.repo.GovernanceSummary(r.Context(), "console", orgID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "governance_unavailable", "Could not load workspace governance summary")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) recomputeAdminUsageRollup(w http.ResponseWriter, r *http.Request) {
	if err := s.repo.RecomputeUsageDailyRollups(r.Context(), ""); err != nil {
		writeError(w, r, http.StatusInternalServerError, "rollup_failed", "Could not recompute usage rollups")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recomputed"})
}

func (s *Server) recomputeConsoleUsageRollup(w http.ResponseWriter, r *http.Request) {
	_, orgID, ok := s.consoleWorkspaceForMutation(w, r)
	if !ok {
		return
	}
	if err := s.repo.RecomputeUsageDailyRollups(r.Context(), orgID); err != nil {
		writeError(w, r, http.StatusInternalServerError, "rollup_failed", "Could not recompute usage rollups")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recomputed"})
}

func (s *Server) consoleWorkspace(w http.ResponseWriter, r *http.Request) (store.PublicUser, string, bool) {
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return store.PublicUser{}, "", false
	}
	orgID, _, ok := s.consoleWorkspaceForRead(w, r, user)
	if !ok {
		return store.PublicUser{}, "", false
	}
	return user, orgID, true
}

func (s *Server) consoleWorkspaceForMutation(w http.ResponseWriter, r *http.Request) (store.PublicUser, string, bool) {
	user, err := s.userFromRequest(r)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "Login required")
		return store.PublicUser{}, "", false
	}
	orgID, _, ok := s.consoleWorkspaceForOperate(w, r, user)
	if !ok {
		return store.PublicUser{}, "", false
	}
	return user, orgID, true
}

func auditQueryFromRequest(r *http.Request, orgID string) store.AuditQuery {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	from, _ := parseAuditTime(q.Get("from"), false)
	to, _ := parseAuditTime(q.Get("to"), true)
	return store.AuditQuery{
		OrgID:      orgID,
		EventClass: strings.TrimSpace(q.Get("eventClass")),
		Actor:      strings.TrimSpace(q.Get("actor")),
		Action:     strings.TrimSpace(q.Get("action")),
		ObjectType: strings.TrimSpace(q.Get("objectType")),
		ObjectID:   strings.TrimSpace(q.Get("objectId")),
		Result:     strings.TrimSpace(q.Get("result")),
		Query:      strings.TrimSpace(q.Get("query")),
		From:       from,
		To:         to,
		Limit:      limit,
	}
}

func incidentQueryFromRequest(r *http.Request, orgID string) store.IncidentQuery {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	return store.IncidentQuery{
		OrgID:  orgID,
		Status: strings.TrimSpace(q.Get("status")),
		Query:  strings.TrimSpace(q.Get("q")),
		Limit:  limit,
	}
}

func incidentActionMessage(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Body == nil || r.ContentLength == 0 {
		return "", true
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body")
		return "", false
	}
	return strings.TrimSpace(req.Message), true
}

func hasBulkSelection(ids []string) bool {
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			return true
		}
	}
	return false
}

func writeIncidentMutationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidIncidentInput):
		writeError(w, r, http.StatusBadRequest, "invalid_incident", "Channel, title and status are required")
	case errors.Is(err, store.ErrOpenIncidentExists):
		writeError(w, r, http.StatusConflict, "open_incident_exists", "This channel already has an open incident")
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, r, http.StatusNotFound, "incident_not_found", "Incident or channel not found")
	default:
		writeError(w, r, http.StatusBadRequest, "incident_mutation_failed", err.Error())
	}
}

func parseAuditTime(value string, endOfDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		return t.Add(24*time.Hour - time.Nanosecond), nil
	}
	return t, nil
}

func (s *Server) exportAudit(w http.ResponseWriter, r *http.Request, orgID string) {
	query := auditQueryFromRequest(r, orgID)
	query.Limit = 500
	result, err := s.repo.AuditLogs(r.Context(), query)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "audit_export_failed", "Could not export audit logs")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tokhub-audit.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"created_at", "actor_email", "action", "object_type", "object_id", "result", "ip"})
	for _, item := range result.Items {
		_ = writer.Write([]string{
			item.CreatedAt.Format("2006-01-02 15:04:05"),
			item.ActorEmail,
			item.Action,
			item.ObjectType,
			item.ObjectID,
			item.Result,
			item.IP,
		})
	}
	writer.Flush()
}
