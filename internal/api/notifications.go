package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	stdmail "net/mail"
	"net/url"
	"strings"
	"time"

	"tokhub/internal/store"
)

func (s *Server) deliverAlertNotification(ctx context.Context, delivery store.AlertDelivery) store.AlertDelivery {
	switch delivery.Status {
	case "sent", "test", "recovered":
	default:
		return delivery
	}
	if strings.TrimSpace(delivery.NotificationChannelID) == "" {
		return delivery
	}
	channel, err := s.repo.NotificationChannelByID(ctx, delivery.Scope, delivery.OrgID, delivery.NotificationChannelID)
	if err != nil {
		updated, updateErr := s.repo.UpdateAlertDeliverySendResult(ctx, delivery.ID, "failed", "notification channel unavailable", "none", map[string]any{"delivery_error": err.Error()})
		if updateErr == nil {
			return updated
		}
		delivery.Status = "failed"
		delivery.Error = "notification channel unavailable"
		return delivery
	}
	channel, err = s.notificationChannelWithPlainTarget(channel)
	if err != nil {
		updated, updateErr := s.repo.UpdateAlertDeliverySendResult(ctx, delivery.ID, "failed", "notification target unavailable", "none", map[string]any{"delivery_error": err.Error()})
		if updateErr == nil {
			return updated
		}
		delivery.Status = "failed"
		delivery.Error = "notification target unavailable"
		return delivery
	}
	status, errorText, deliveredBy, metadata := s.sendNotification(ctx, channel, delivery)
	updated, err := s.repo.UpdateAlertDeliverySendResult(ctx, delivery.ID, status, errorText, deliveredBy, metadata)
	if delivery.Status == "test" {
		_ = s.repo.MarkNotificationChannelTested(ctx, channel.ID, errorText)
	}
	if err != nil {
		delivery.Status = status
		delivery.Error = errorText
		return delivery
	}
	return updated
}

func (s *Server) notificationChannelWithPlainTarget(channel store.NotificationChannel) (store.NotificationChannel, error) {
	if strings.TrimSpace(channel.Target) != "" {
		return channel, nil
	}
	if strings.TrimSpace(channel.TargetCiphertext) == "" || strings.TrimSpace(channel.TargetNonce) == "" {
		return channel, nil
	}
	if s.secretBox == nil {
		return channel, fmt.Errorf("notification target encryption is unavailable")
	}
	plain, err := s.secretBox.Decrypt(channel.TargetCiphertext, channel.TargetNonce)
	if err != nil {
		return channel, err
	}
	channel.Target = plain
	channel.TargetMask = store.MaskNotificationTarget(channel.Type, plain)
	return channel, nil
}

func (s *Server) backfillNotificationChannelTargets() {
	if s.secretBox == nil || s.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	targets, err := s.repo.LegacyNotificationChannelTargets(ctx)
	if err != nil {
		s.logger.Warn("notification target secret backfill skipped", "error", err)
		return
	}
	converted := 0
	for _, item := range targets {
		encrypted, err := s.secretBox.Encrypt(item.Target)
		if err != nil {
			s.logger.Warn("notification target encryption failed", "channel_id", item.ID, "error", err)
			continue
		}
		err = s.repo.StoreNotificationChannelEncryptedTarget(ctx, item.ID, store.EncryptedCredential{
			Ciphertext:  encrypted.Ciphertext,
			Nonce:       encrypted.Nonce,
			Fingerprint: encrypted.Fingerprint,
			Mask:        store.MaskNotificationTarget(item.Type, item.Target),
		})
		if err != nil {
			s.logger.Warn("notification target secret backfill failed", "channel_id", item.ID, "error", err)
			continue
		}
		converted++
	}
	if converted > 0 {
		s.logger.Info("notification targets encrypted", "count", converted)
	}
}

func (s *Server) deliverAlertNotifications(ctx context.Context, deliveries []store.AlertDelivery) []store.AlertDelivery {
	out := make([]store.AlertDelivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		out = append(out, s.deliverAlertNotification(ctx, delivery))
	}
	return out
}

func (s *Server) sendNotification(ctx context.Context, channel store.NotificationChannel, delivery store.AlertDelivery) (string, string, string, map[string]any) {
	metadata := map[string]any{
		"notification_type": channel.Type,
		"target_host":       notificationTargetHost(channel.Target),
	}
	if !channel.Enabled {
		return "failed", "notification channel is disabled", "none", metadata
	}
	switch channel.Type {
	case "webhook", "feishu":
		return s.sendWebhookNotification(ctx, channel, delivery, metadata)
	default:
		if !validEmailTarget(channel.Target) {
			return "failed", "invalid email target", "email_outbox", metadata
		}
		return s.sendEmailNotification(ctx, channel, delivery, metadata)
	}
}

func (s *Server) sendEmailNotification(ctx context.Context, channel store.NotificationChannel, delivery store.AlertDelivery, metadata map[string]any) (string, string, string, map[string]any) {
	subject := fmt.Sprintf("[TokHub][%s] %s", delivery.Severity, delivery.Title)
	body := fmt.Sprintf("%s\n\n状态: %s\n范围: %s\n组织: %s\nDelivery: %s\n", delivery.Message, delivery.Status, delivery.Scope, delivery.OrgID, delivery.ID)
	status, errText, deliveredBy, mailMeta := s.sendMail(ctx, channel.Target, subject, body)
	for key, value := range mailMeta {
		metadata[key] = value
	}
	if status == "failed" {
		return "failed", errText, deliveredBy, metadata
	}
	return delivery.Status, "", deliveredBy, metadata
}

func (s *Server) sendAuthMail(ctx context.Context, to string, subject string, body string) (string, string, string, map[string]any) {
	return s.sendMail(ctx, to, subject, body)
}

func (s *Server) sendWebhookNotification(ctx context.Context, channel store.NotificationChannel, delivery store.AlertDelivery, metadata map[string]any) (string, string, string, map[string]any) {
	parsed, err := url.Parse(strings.TrimSpace(channel.Target))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "failed", "invalid webhook target", channel.Type, metadata
	}
	payload := map[string]any{
		"title":      delivery.Title,
		"message":    delivery.Message,
		"severity":   delivery.Severity,
		"status":     delivery.Status,
		"scope":      delivery.Scope,
		"orgId":      delivery.OrgID,
		"deliveryId": delivery.ID,
		"createdAt":  delivery.CreatedAt,
	}
	raw, _ := json.Marshal(payload)
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, parsed.String(), bytes.NewReader(raw))
	if err != nil {
		return "failed", err.Error(), channel.Type, metadata
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TokHub-Notifier/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "failed", err.Error(), channel.Type, metadata
	}
	defer resp.Body.Close()
	metadata["http_status"] = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "failed", fmt.Sprintf("webhook returned HTTP %d", resp.StatusCode), channel.Type, metadata
	}
	return delivery.Status, "", channel.Type, metadata
}

func validEmailTarget(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return false
	}
	addr, err := stdmail.ParseAddress(value)
	if err != nil || addr.Address != value {
		return false
	}
	local, domain, ok := strings.Cut(addr.Address, "@")
	return ok && local != "" && strings.Contains(domain, ".") && !strings.ContainsAny(domain, " \t")
}

func notificationTargetHost(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err == nil && parsed.Host != "" {
		return parsed.Host
	}
	if idx := strings.LastIndex(value, "@"); idx >= 0 && idx+1 < len(value) {
		return value[idx+1:]
	}
	return ""
}
