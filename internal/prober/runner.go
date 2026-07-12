package prober

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	secretcrypto "tokhub/internal/crypto"
	"tokhub/internal/store"
)

var (
	ErrProbeCredentialInvalid = errors.New("probe credential invalid")
	ErrUnsupportedProbeLayer  = errors.New("unsupported probe layer")
)

const probePersistenceTimeout = 5 * time.Second

type Runner struct {
	repo      *store.Repository
	executor  *Executor
	secretBox *secretcrypto.SecretBox
	logger    *slog.Logger
}

func NewRunner(repo *store.Repository, logger *slog.Logger) *Runner {
	return NewRunnerWithMockEndpoints(repo, logger, true)
}

func NewRunnerWithMockEndpoints(repo *store.Repository, logger *slog.Logger, mockEndpoints bool) *Runner {
	return &Runner{repo: repo, executor: NewExecutorWithMockEndpoints(mockEndpoints), logger: logger}
}

func NewRunnerWithSecretKey(repo *store.Repository, logger *slog.Logger, mockEndpoints bool, secretKey string) (*Runner, error) {
	box, err := secretcrypto.NewSecretBox(secretKey)
	if err != nil {
		return nil, err
	}
	return &Runner{repo: repo, executor: NewExecutorWithMockEndpoints(mockEndpoints), secretBox: box, logger: logger}, nil
}

func (r *Runner) RunLayer(ctx context.Context, channelID string, layer string, source string) error {
	return r.RunLayerWithCredential(ctx, channelID, layer, source, "")
}

func (r *Runner) RunLayerWithCredential(ctx context.Context, channelID string, layer string, source string, apiKey string) error {
	runID := fmt.Sprintf("pr_%s_%s_%s", layer, channelID, uuid.NewString())
	return r.RunLayerWithIDAndCredential(ctx, runID, channelID, layer, source, apiKey)
}

func (r *Runner) RunLayerWithID(ctx context.Context, runID string, channelID string, layer string, source string) error {
	return r.RunLayerWithIDAndCredential(ctx, runID, channelID, layer, source, "")
}

func (r *Runner) RunLayerWithIDAndCredential(ctx context.Context, runID string, channelID string, layer string, source string, apiKey string) error {
	if !isSupportedProbeLayer(layer) {
		return fmt.Errorf("%w: %s", ErrUnsupportedProbeLayer, layer)
	}
	target, err := r.repo.ProbeTarget(ctx, channelID)
	if err != nil {
		return err
	}
	if err := r.repo.UpsertProbeRun(ctx, runID, channelID, layer, source, probeRunSnapshot(target)); err != nil {
		return err
	}

	probeTarget := ProbeTarget{
		ID:             target.ID,
		OwnerType:      target.OwnerType,
		OwnerID:        target.OwnerID,
		Name:           target.Name,
		Provider:       target.Provider,
		Type:           target.Type,
		Endpoint:       target.Endpoint,
		Status:         target.Status,
		Model:          target.Model,
		ProviderConfig: target.ProviderConfig,
	}
	probeKey := ""
	needsCredential := probeLayerNeedsCredential(layer)
	if layer == "l2" && l2ProbeMode(probeTarget) == "skip" {
		needsCredential = false
	}
	if needsCredential {
		probeKey, err = r.probeCredential(ctx, target, apiKey)
		if err != nil {
			_, persistErr := r.persistProbeOutcome(ctx, runID, channelID, layer, credentialFailureSteps(layer))
			if persistErr != nil {
				return fmt.Errorf("%w; persist failed probe run: %v", err, persistErr)
			}
			return err
		}
	}
	var steps []StepResult
	switch layer {
	case "l1":
		steps = r.executor.L1(ctx, probeTarget)
	case "l2":
		steps = r.executor.L2(ctx, probeTarget, probeKey)
	case "l3":
		steps = r.executor.L3(ctx, probeTarget, probeKey)
	}

	summary, err := r.persistProbeOutcome(ctx, runID, channelID, layer, steps)
	if err != nil {
		return err
	}
	r.logger.Info("probe layer complete", "channel_id", channelID, "layer", layer, "status", summary.Status, "run_id", runID)
	return nil
}

func probeRunSnapshot(target store.ProbeTarget) store.ProbeRunSnapshot {
	return store.ProbeRunSnapshot{
		ChannelName: target.Name,
		Provider:    target.Provider,
		Type:        target.Type,
		Endpoint:    probeSnapshotEndpoint(target.Endpoint),
		Model:       target.Model,
	}
}

func probeSnapshotEndpoint(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (r *Runner) persistProbeOutcome(ctx context.Context, runID string, channelID string, layer string, steps []StepResult) (LayerSummary, error) {
	storeSteps := make([]store.ProbeStepResult, 0, len(steps))
	for _, step := range steps {
		storeSteps = append(storeSteps, store.ProbeStepResult{
			Step:       step.Step,
			Status:     step.Status,
			LatencyMs:  step.LatencyMs,
			HTTPStatus: step.HTTPStatus,
			ErrorType:  step.ErrorType,
			Metadata:   step.Metadata,
		})
	}
	writeCtx, cancel := probePersistenceContext(ctx)
	defer cancel()
	if err := r.repo.UpsertProbeResults(writeCtx, runID, channelID, layer, storeSteps); err != nil {
		return LayerSummary{}, err
	}
	summary := Summarize(steps)
	runStatus := "success"
	if summary.Status == "down" || summary.Status == "auth_error" {
		runStatus = "failed"
	}
	if err := r.repo.CompleteProbeRun(writeCtx, runID, runStatus, summary.ErrorType); err != nil {
		return LayerSummary{}, err
	}
	if err := r.applyCurrentStatus(writeCtx, channelID); err != nil {
		return LayerSummary{}, err
	}
	return summary, nil
}

func probePersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), probePersistenceTimeout)
}

func credentialFailureSteps(layer string) []StepResult {
	stepName := "probe"
	switch layer {
	case "l2":
		stepName = "models"
	case "l3":
		stepName = "generate"
	}
	return []StepResult{{
		Step:      stepName,
		Status:    "auth_error",
		ErrorType: "auth_error",
		Metadata:  map[string]any{"reason": "credential_invalid"},
	}}
}

func isSupportedProbeLayer(layer string) bool {
	switch layer {
	case "l1", "l2", "l3":
		return true
	default:
		return false
	}
}

func probeLayerNeedsCredential(layer string) bool {
	return layer == "l2" || layer == "l3"
}

func (r *Runner) probeCredential(ctx context.Context, target store.ProbeTarget, provided string) (string, error) {
	provided = strings.TrimSpace(provided)
	if provided != "" {
		return provided, nil
	}
	if r.secretBox == nil {
		return "", nil
	}
	switch target.OwnerType {
	case "platform":
		cred, err := r.repo.PlatformCredential(ctx, target.ID)
		if err != nil {
			return "", nil
		}
		plain, err := r.secretBox.Decrypt(cred.Ciphertext, cred.Nonce)
		if err != nil {
			return "", fmt.Errorf("%w: platform channel %s: %v", ErrProbeCredentialInvalid, target.ID, err)
		}
		return plain, nil
	case "user":
		orgID := strings.TrimSpace(target.OrgID)
		if orgID == "" {
			orgID = store.PersonalOrgID(target.OwnerID)
		}
		cred, err := r.repo.PrivateCredentialForOrg(ctx, orgID, target.ID)
		if err != nil {
			return "", nil
		}
		plain, err := r.secretBox.Decrypt(cred.Ciphertext, cred.Nonce)
		if err != nil {
			return "", fmt.Errorf("%w: private channel %s: %v", ErrProbeCredentialInvalid, target.ID, err)
		}
		return plain, nil
	default:
		return "", nil
	}
}

func (r *Runner) ProbeNow(ctx context.Context, channelID string, source string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := r.RunLayer(probeCtx, channelID, "l1", source); err != nil {
		return err
	}
	return r.RunLayer(probeCtx, channelID, "l2", source)
}

func (r *Runner) ProbeNowWithL3(ctx context.Context, channelID string, source string, apiKey string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := r.RunLayer(probeCtx, channelID, "l1", source); err != nil {
		return err
	}
	if err := r.RunLayerWithCredential(probeCtx, channelID, "l2", source, apiKey); err != nil {
		return err
	}
	return r.RunLayerWithCredential(probeCtx, channelID, "l3", source, apiKey)
}

func (r *Runner) applyCurrentStatus(ctx context.Context, channelID string) error {
	l1, err := r.repo.LatestLayerSummary(ctx, channelID, "l1")
	if err != nil {
		return err
	}
	l2, err := r.repo.LatestLayerSummary(ctx, channelID, "l2")
	if err != nil {
		return err
	}
	l3, err := r.repo.LatestLayerSummary(ctx, channelID, "l3")
	if err != nil {
		return err
	}
	decision := SynthesizeStatusWithL3(
		LayerSummary{Status: l1.Status, LatencyMs: l1.LatencyMs, ErrorType: l1.ErrorType},
		LayerSummary{Status: l2.Status, LatencyMs: l2.LatencyMs, ErrorType: l2.ErrorType},
		LayerSummary{Status: l3.Status, LatencyMs: l3.LatencyMs, ErrorType: l3.ErrorType},
	)
	return r.repo.ApplyProbeStatusWithL3(ctx, channelID, decision.Status, decision.ErrorType, l1, l2, l3)
}
