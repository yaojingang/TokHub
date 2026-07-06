package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"tokhub/internal/prober"
	"tokhub/internal/store"
)

const (
	SubjectProbeTaskL1       = "probe.task.l1"
	SubjectProbeTaskL2       = "probe.task.l2"
	SubjectProbeTaskL3       = "probe.task.l3"
	SubjectProbeStatusChange = "probe.status.changed"
)

const (
	probeSchedulerInterval   = 30 * time.Second
	schedulerTaskMaxAge      = 2 * time.Minute
	newProbeTargetWindow     = 24 * time.Hour
	newProbeTargetL3Interval = 8 * time.Hour
	healthyL1Interval        = 15 * time.Minute
	healthyL2Interval        = 4 * time.Hour
	healthyL3Interval        = 24 * time.Hour
	unhealthyL1Interval      = 5 * time.Minute
	unhealthyL2Interval      = 30 * time.Minute
	unhealthyL3Interval      = 6 * time.Hour
	authErrorL1Interval      = time.Hour
	authErrorL2Interval      = 6 * time.Hour
	authErrorL3Interval      = 24 * time.Hour
	unknownL1Interval        = 5 * time.Minute
	unknownL2Interval        = 30 * time.Minute
	unknownL3Interval        = 24 * time.Hour
	schedulerMaxTasksPerTick = 1
	probeTaskAckWait         = 5 * time.Minute
)

var probeLayers = []string{"l1", "l2", "l3"}

type ProbeTask struct {
	RunID     string `json:"runId"`
	ChannelID string `json:"channelId"`
	Layer     string `json:"layer"`
	Source    string `json:"source"`
}

type ProbeRuntime struct {
	nc     *nats.Conn
	js     nats.JetStreamContext
	repo   *store.Repository
	runner *prober.Runner
	logger *slog.Logger

	probeSlots            chan struct{}
	schedulerTargetCursor int
	terminalProbeTaskLogs sync.Map
}

type scheduledProbeTask struct {
	ChannelID string
	Layer     string
}

func StartProbeRuntime(ctx context.Context, natsURL string, repo *store.Repository, runner *prober.Runner, logger *slog.Logger, enableScheduler bool) (*ProbeRuntime, error) {
	nc, err := nats.Connect(natsURL, nats.Name("tokhub-prober"), nats.Timeout(5*time.Second))
	if err != nil {
		return nil, err
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, err
	}
	_, _ = js.AddStream(&nats.StreamConfig{
		Name:     "TOKHUB_PROBE",
		Subjects: []string{"probe.task.*", SubjectProbeStatusChange},
		Storage:  nats.FileStorage,
	})
	runtime := &ProbeRuntime{nc: nc, js: js, repo: repo, runner: runner, logger: logger, probeSlots: make(chan struct{}, 1)}
	if err := runtime.subscribe(ctx, SubjectProbeTaskL1, "tokhub-prober-l1"); err != nil {
		nc.Close()
		return nil, err
	}
	if err := runtime.subscribe(ctx, SubjectProbeTaskL2, "tokhub-prober-l2"); err != nil {
		nc.Close()
		return nil, err
	}
	if err := runtime.subscribe(ctx, SubjectProbeTaskL3, "tokhub-prober-l3"); err != nil {
		nc.Close()
		return nil, err
	}
	if enableScheduler {
		go runtime.scheduler(ctx)
	}
	return runtime, nil
}

func (r *ProbeRuntime) Close() {
	if r.nc != nil {
		r.nc.Drain()
		r.nc.Close()
	}
}

func (r *ProbeRuntime) PublishTask(channelID string, layer string, source string) error {
	task := ProbeTask{
		RunID:     fmt.Sprintf("pr_%s_%s_%s", layer, channelID, uuid.NewString()),
		ChannelID: channelID,
		Layer:     layer,
		Source:    source,
	}
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = r.js.Publish("probe.task."+layer, raw)
	return err
}

func (r *ProbeRuntime) subscribe(ctx context.Context, subject string, durable string) error {
	err := r.queueSubscribe(ctx, subject, durable)
	if err == nil {
		return nil
	}
	if !probeConsumerConfigMismatch(err) {
		return err
	}
	r.logger.Warn("recreating probe consumer after config mismatch", "subject", subject, "durable", durable, "error", err)
	if deleteErr := r.js.DeleteConsumer("TOKHUB_PROBE", durable); deleteErr != nil && !errors.Is(deleteErr, nats.ErrConsumerNotFound) {
		return fmt.Errorf("delete mismatched probe consumer %s: %w", durable, deleteErr)
	}
	return r.queueSubscribe(ctx, subject, durable)
}

func (r *ProbeRuntime) queueSubscribe(ctx context.Context, subject string, durable string) error {
	_, err := r.js.QueueSubscribe(subject, "tokhub-probers", func(msg *nats.Msg) {
		var task ProbeTask
		if err := json.Unmarshal(msg.Data, &task); err != nil {
			_ = msg.Term()
			return
		}
		if task.Layer == "" {
			task.Layer = layerFromSubject(msg.Subject)
		}
		if staleProbeTaskMessage(msg, task, time.Now()) {
			_ = msg.Term()
			return
		}
		if !r.acquireProbeSlot(ctx) {
			_ = msg.Nak()
			return
		}
		defer r.releaseProbeSlot()
		if staleProbeTaskMessage(msg, task, time.Now()) {
			_ = msg.Term()
			return
		}
		runCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		err := r.runner.RunLayerWithID(runCtx, task.RunID, task.ChannelID, task.Layer, task.Source)
		cancel()
		if err != nil {
			if reason, ok := terminalProbeTaskErrorReason(err); ok {
				r.logTerminalProbeTask(task, reason, err)
				_ = msg.Term()
				return
			}
			r.logger.Error("probe task failed", "channel_id", task.ChannelID, "layer", task.Layer, "error", err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	}, nats.Durable(durable), nats.ManualAck(), nats.AckWait(probeTaskAckWait), nats.MaxAckPending(1))
	return err
}

func probeConsumerConfigMismatch(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "consumer's value") || strings.Contains(text, "configuration requests")
}

func (r *ProbeRuntime) acquireProbeSlot(ctx context.Context) bool {
	if r.probeSlots == nil {
		return true
	}
	select {
	case r.probeSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *ProbeRuntime) releaseProbeSlot() {
	if r.probeSlots == nil {
		return
	}
	select {
	case <-r.probeSlots:
	default:
	}
}

func staleProbeTaskMessage(msg *nats.Msg, task ProbeTask, now time.Time) bool {
	if task.Source != "scheduler" {
		return false
	}
	meta, err := msg.Metadata()
	if err != nil || meta == nil {
		return false
	}
	return staleSchedulerTask(task, meta.Timestamp, now)
}

func staleSchedulerTask(task ProbeTask, publishedAt time.Time, now time.Time) bool {
	if task.Source != "scheduler" || publishedAt.IsZero() || publishedAt.After(now) {
		return false
	}
	return now.Sub(publishedAt) > schedulerTaskMaxAge
}

func terminalProbeTaskErrorReason(err error) (string, bool) {
	switch {
	case errors.Is(err, store.ErrProbeTargetNotFound):
		return "probe_target_not_found", true
	case errors.Is(err, prober.ErrProbeCredentialInvalid):
		return "probe_credential_invalid", true
	case errors.Is(err, prober.ErrUnsupportedProbeLayer):
		return "unsupported_probe_layer", true
	default:
		return "", false
	}
}

func (r *ProbeRuntime) logTerminalProbeTask(task ProbeTask, reason string, err error) {
	key := task.ChannelID + "\x00" + task.Layer + "\x00" + reason
	if _, loaded := r.terminalProbeTaskLogs.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	r.logger.Warn("probe task terminated", "channel_id", task.ChannelID, "layer", task.Layer, "reason", reason, "error", err)
}

func (r *ProbeRuntime) scheduler(ctx context.Context) {
	r.publishDue(ctx, "scheduler", time.Now())
	ticker := time.NewTicker(probeSchedulerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.publishDue(ctx, "scheduler", now)
		}
	}
}

func (r *ProbeRuntime) publishDue(ctx context.Context, source string, now time.Time) {
	targets, err := r.repo.ProbeScheduleTargets(ctx)
	if err != nil {
		r.logger.Error("load probe schedule targets", "error", err)
		return
	}
	counts := map[string]int{"l1": 0, "l2": 0, "l3": 0}
	tasks, nextCursor := scheduledProbeTasks(targets, now, r.schedulerTargetCursor)
	r.schedulerTargetCursor = nextCursor
	for _, task := range tasks {
		if err := r.PublishTask(task.ChannelID, task.Layer, source); err != nil {
			r.logger.Error("publish probe task", "channel_id", task.ChannelID, "layer", task.Layer, "error", err)
			continue
		}
		counts[task.Layer]++
	}
	r.logger.Info("due probe tasks scheduled", "channels", len(targets), "l1", counts["l1"], "l2", counts["l2"], "l3", counts["l3"], "source", source, "limit", schedulerMaxTasksPerTick)
}

func scheduledProbeTasks(targets []store.ProbeScheduleTarget, now time.Time, cursor int) ([]scheduledProbeTask, int) {
	if len(targets) == 0 {
		return nil, 0
	}
	if cursor < 0 || cursor >= len(targets) {
		cursor = 0
	}
	tasks := make([]scheduledProbeTask, 0, schedulerMaxTasksPerTick)
	nextCursor := appendMissingScheduledProbeTasks(targets, &tasks, cursor)
	if len(tasks) < schedulerMaxTasksPerTick {
		nextCursor = appendScheduledProbeTasks(targets, &tasks, nextCursor, func(target store.ProbeScheduleTarget) (string, bool) {
			return firstDueProbeLayer(target, now)
		})
	}
	return tasks, nextCursor
}

func appendMissingScheduledProbeTasks(targets []store.ProbeScheduleTarget, tasks *[]scheduledProbeTask, cursor int) int {
	nextCursor := cursor
	for _, layer := range probeLayers {
		for checked := 0; checked < len(targets) && len(*tasks) < schedulerMaxTasksPerTick; checked++ {
			index := (cursor + checked) % len(targets)
			target := targets[index]
			if !probeLayerMissing(target, layer) {
				continue
			}
			*tasks = append(*tasks, scheduledProbeTask{ChannelID: target.ID, Layer: layer})
			nextCursor = (index + 1) % len(targets)
		}
		if len(*tasks) >= schedulerMaxTasksPerTick {
			break
		}
	}
	return nextCursor
}

func appendScheduledProbeTasks(targets []store.ProbeScheduleTarget, tasks *[]scheduledProbeTask, cursor int, layerFn func(store.ProbeScheduleTarget) (string, bool)) int {
	nextCursor := cursor
	for checked := 0; checked < len(targets) && len(*tasks) < schedulerMaxTasksPerTick; checked++ {
		index := (cursor + checked) % len(targets)
		target := targets[index]
		layer, ok := layerFn(target)
		if !ok {
			continue
		}
		*tasks = append(*tasks, scheduledProbeTask{ChannelID: target.ID, Layer: layer})
		nextCursor = (index + 1) % len(targets)
	}
	return nextCursor
}

func probeLayerMissing(target store.ProbeScheduleTarget, layer string) bool {
	lastProbeAt := target.LastProbeAt(layer)
	return lastProbeAt.IsZero() || lastProbeAt.Unix() == 0
}

func firstDueProbeLayer(target store.ProbeScheduleTarget, now time.Time) (string, bool) {
	for _, layer := range probeLayers {
		if probeLayerDue(target, layer, now) {
			return layer, true
		}
	}
	return "", false
}

func probeLayerDue(target store.ProbeScheduleTarget, layer string, now time.Time) bool {
	if probeLayerMissing(target, layer) {
		return true
	}
	lastProbeAt := target.LastProbeAt(layer)
	interval := probeLayerInterval(target, layer, now)
	if interval <= 0 {
		return false
	}
	return !lastProbeAt.After(now) && now.Sub(lastProbeAt) >= interval
}

func probeLayerInterval(target store.ProbeScheduleTarget, layer string, now time.Time) time.Duration {
	var l1, l2, l3 time.Duration
	switch target.Status {
	case "healthy":
		l1, l2, l3 = healthyL1Interval, healthyL2Interval, healthyL3Interval
	case "degraded", "connectivity_down", "functional_down":
		l1, l2, l3 = unhealthyL1Interval, unhealthyL2Interval, unhealthyL3Interval
	case "auth_error":
		l1, l2, l3 = authErrorL1Interval, authErrorL2Interval, authErrorL3Interval
	default:
		l1, l2, l3 = unknownL1Interval, unknownL2Interval, unknownL3Interval
	}
	if layer == "l3" && isNewProbeTarget(target, now) && target.Status != "auth_error" && l3 > newProbeTargetL3Interval {
		l3 = newProbeTargetL3Interval
	}
	switch layer {
	case "l1":
		return l1
	case "l2":
		return l2
	case "l3":
		return l3
	default:
		return 0
	}
}

func isNewProbeTarget(target store.ProbeScheduleTarget, now time.Time) bool {
	if target.CreatedAt.IsZero() || target.CreatedAt.After(now) {
		return true
	}
	return now.Sub(target.CreatedAt) <= newProbeTargetWindow
}

func layerFromSubject(subject string) string {
	switch subject {
	case SubjectProbeTaskL1:
		return "l1"
	case SubjectProbeTaskL2:
		return "l2"
	case SubjectProbeTaskL3:
		return "l3"
	default:
		return "l1"
	}
}
