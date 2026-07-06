package events

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"tokhub/internal/prober"
	"tokhub/internal/store"
)

func TestIsTerminalProbeTaskError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "missing probe target is terminal",
			err:  fmt.Errorf("run probe: %w", store.ErrProbeTargetNotFound),
			want: true,
		},
		{
			name: "invalid stored credential is terminal",
			err:  fmt.Errorf("run probe: %w", prober.ErrProbeCredentialInvalid),
			want: true,
		},
		{
			name: "unsupported layer is terminal",
			err:  fmt.Errorf("run probe: %w", prober.ErrUnsupportedProbeLayer),
			want: true,
		},
		{
			name: "deadline remains retryable",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "generic upstream failure remains retryable",
			err:  errors.New("temporary upstream failure"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := terminalProbeTaskErrorReason(tt.err)
			if got != tt.want {
				t.Fatalf("terminalProbeTaskErrorReason() terminal = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProbeConsumerConfigMismatch(t *testing.T) {
	if !probeConsumerConfigMismatch(errors.New("nats: configuration requests ack wait to be 5m0s, but consumer's value is 30s")) {
		t.Fatalf("expected NATS consumer config mismatch to be detected")
	}
	if probeConsumerConfigMismatch(errors.New("nats: authorization violation")) {
		t.Fatalf("authorization errors must not be treated as consumer config mismatch")
	}
}

func TestProbeTaskAckWaitCoversSerialBacklog(t *testing.T) {
	if probeTaskAckWait <= schedulerTaskMaxAge {
		t.Fatalf("probeTaskAckWait = %s must exceed scheduler stale window %s", probeTaskAckWait, schedulerTaskMaxAge)
	}
}

func TestProbeSchedulerCadenceCoversCurrentPlatformFleet(t *testing.T) {
	currentPlatformChannels := 26
	requiredHealthyL1PerHour := currentPlatformChannels * int(time.Hour/healthyL1Interval)
	capacityPerHour := int(time.Hour / probeSchedulerInterval)
	if capacityPerHour < requiredHealthyL1PerHour {
		t.Fatalf("scheduler capacity = %d/hour, want at least %d/hour for healthy l1 coverage", capacityPerHour, requiredHealthyL1PerHour)
	}
}

func TestProbeLayerDueHealthyL3WaitsTwentyFourHours(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	target := store.ProbeScheduleTarget{
		ProbeTarget: store.ProbeTarget{Status: "healthy"},
		CreatedAt:   now.Add(-48 * time.Hour),
		LastL3At:    now.Add(-23*time.Hour - 59*time.Minute),
	}
	if probeLayerDue(target, "l3", now) {
		t.Fatalf("healthy l3 should not be due before 24h")
	}
	target.LastL3At = now.Add(-24 * time.Hour)
	if !probeLayerDue(target, "l3", now) {
		t.Fatalf("healthy l3 should be due at 24h")
	}
}

func TestProbeLayerDueUnhealthyL3EverySixHours(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{"degraded", "connectivity_down", "functional_down"} {
		t.Run(status, func(t *testing.T) {
			target := store.ProbeScheduleTarget{
				ProbeTarget: store.ProbeTarget{Status: status},
				CreatedAt:   now.Add(-48 * time.Hour),
				LastL3At:    now.Add(-6 * time.Hour),
			}
			if !probeLayerDue(target, "l3", now) {
				t.Fatalf("%s l3 should be due at 6h", status)
			}
		})
	}
}

func TestProbeLayerDueUnknownOrNoHistoryRunsImmediately(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	target := store.ProbeScheduleTarget{
		ProbeTarget: store.ProbeTarget{Status: "unknown"},
		CreatedAt:   now.Add(-time.Hour),
	}
	for _, layer := range probeLayers {
		if !probeLayerDue(target, layer, now) {
			t.Fatalf("%s should be due when no probe history exists", layer)
		}
	}
}

func TestProbeLayerDueRecentRunDefersScheduler(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	target := store.ProbeScheduleTarget{
		ProbeTarget: store.ProbeTarget{Status: "healthy"},
		CreatedAt:   now.Add(-48 * time.Hour),
		LastL3At:    now.Add(-time.Hour),
	}
	if probeLayerDue(target, "l3", now) {
		t.Fatalf("recent l3 run should defer scheduler regardless of source")
	}
}

func TestProbeLayerDueNewHealthyL3UsesEightHourObservation(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	target := store.ProbeScheduleTarget{
		ProbeTarget: store.ProbeTarget{Status: "healthy"},
		CreatedAt:   now.Add(-2 * time.Hour),
		LastL3At:    now.Add(-7*time.Hour - 59*time.Minute),
	}
	if probeLayerDue(target, "l3", now) {
		t.Fatalf("new healthy l3 should not be due before 8h")
	}
	target.LastL3At = now.Add(-8 * time.Hour)
	if !probeLayerDue(target, "l3", now) {
		t.Fatalf("new healthy l3 should be due at 8h")
	}
}

func TestScheduledProbeTasksLimitsAndRotatesTargets(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	targets := []store.ProbeScheduleTarget{
		{ProbeTarget: store.ProbeTarget{ID: "ch_1", Status: "unknown"}, CreatedAt: now.Add(-time.Hour)},
		{ProbeTarget: store.ProbeTarget{ID: "ch_2", Status: "unknown"}, CreatedAt: now.Add(-time.Hour)},
		{ProbeTarget: store.ProbeTarget{ID: "ch_3", Status: "unknown"}, CreatedAt: now.Add(-time.Hour)},
	}

	tasks, nextCursor := scheduledProbeTasks(targets, now, 0)
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(tasks))
	}
	if tasks[0].ChannelID != "ch_1" || tasks[0].Layer != "l1" {
		t.Fatalf("task = %+v, want ch_1 l1", tasks[0])
	}
	if nextCursor != 1 {
		t.Fatalf("next cursor = %d, want 1", nextCursor)
	}

	tasks, nextCursor = scheduledProbeTasks(targets, now, nextCursor)
	if len(tasks) != 1 || tasks[0].ChannelID != "ch_2" || tasks[0].Layer != "l1" {
		t.Fatalf("second task = %+v, want ch_2 l1", tasks)
	}
	if nextCursor != 2 {
		t.Fatalf("second next cursor = %d, want 2", nextCursor)
	}
}

func TestScheduledProbeTasksPublishesOneLayerPerChannelSweep(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	targets := []store.ProbeScheduleTarget{{
		ProbeTarget: store.ProbeTarget{ID: "ch_new", Status: "unknown"},
		CreatedAt:   now.Add(-time.Hour),
	}}

	tasks, _ := scheduledProbeTasks(targets, now, 0)
	if len(tasks) != 1 || tasks[0].Layer != "l1" {
		t.Fatalf("new target first task = %+v, want only l1", tasks)
	}

	targets[0].LastL1At = now
	tasks, _ = scheduledProbeTasks(targets, now.Add(time.Minute), 0)
	if len(tasks) != 1 || tasks[0].Layer != "l2" {
		t.Fatalf("after l1 task = %+v, want only l2", tasks)
	}
}

func TestScheduledProbeTasksCompletesMissingHistoryBeforeRetesting(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	targets := []store.ProbeScheduleTarget{
		{
			ProbeTarget: store.ProbeTarget{ID: "ch_due_again", Status: "unknown"},
			CreatedAt:   now.Add(-time.Hour),
			LastL1At:    now.Add(-unknownL1Interval),
			LastL2At:    now.Add(-time.Minute),
			LastL3At:    now.Add(-time.Minute),
		},
		{
			ProbeTarget: store.ProbeTarget{ID: "ch_never_probed", Status: "unknown"},
			CreatedAt:   now.Add(-time.Hour),
		},
	}

	tasks, nextCursor := scheduledProbeTasks(targets, now, 0)
	if len(tasks) != 1 || tasks[0].ChannelID != "ch_never_probed" || tasks[0].Layer != "l1" {
		t.Fatalf("task = %+v, want ch_never_probed l1 before retesting ch_due_again", tasks)
	}
	if nextCursor != 0 {
		t.Fatalf("next cursor = %d, want wrap to 0", nextCursor)
	}
}

func TestScheduledProbeTasksPrioritizesMissingL1AcrossTargets(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	targets := []store.ProbeScheduleTarget{
		{
			ProbeTarget: store.ProbeTarget{ID: "ch_missing_l2", Status: "unknown"},
			CreatedAt:   now.Add(-time.Hour),
			LastL1At:    now,
		},
		{
			ProbeTarget: store.ProbeTarget{ID: "ch_missing_l1", Status: "unknown"},
			CreatedAt:   now.Add(-time.Hour),
		},
	}

	tasks, _ := scheduledProbeTasks(targets, now, 0)
	if len(tasks) != 1 || tasks[0].ChannelID != "ch_missing_l1" || tasks[0].Layer != "l1" {
		t.Fatalf("task = %+v, want missing l1 target before missing l2 target", tasks)
	}
}

func TestScheduledProbeTasksSkipsNotDueTargets(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	targets := []store.ProbeScheduleTarget{
		{
			ProbeTarget: store.ProbeTarget{ID: "ch_recent", Status: "healthy"},
			CreatedAt:   now.Add(-48 * time.Hour),
			LastL1At:    now.Add(-time.Minute),
			LastL2At:    now.Add(-time.Minute),
			LastL3At:    now.Add(-time.Minute),
		},
		{
			ProbeTarget: store.ProbeTarget{ID: "ch_due", Status: "unknown"},
			CreatedAt:   now.Add(-time.Hour),
		},
	}

	tasks, nextCursor := scheduledProbeTasks(targets, now, 0)
	if len(tasks) != 1 || tasks[0].ChannelID != "ch_due" {
		t.Fatalf("task = %+v, want ch_due", tasks)
	}
	if nextCursor != 0 {
		t.Fatalf("next cursor = %d, want wrap to 0", nextCursor)
	}
}

func TestStaleSchedulerTaskOnlyDropsOldSchedulerMessages(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if !staleSchedulerTask(ProbeTask{Source: "scheduler"}, now.Add(-schedulerTaskMaxAge-time.Second), now) {
		t.Fatalf("old scheduler task should be stale")
	}
	if staleSchedulerTask(ProbeTask{Source: "scheduler"}, now.Add(-schedulerTaskMaxAge+time.Second), now) {
		t.Fatalf("recent scheduler task should not be stale")
	}
	if staleSchedulerTask(ProbeTask{Source: "manual"}, now.Add(-time.Hour), now) {
		t.Fatalf("manual task should not be stale")
	}
}
