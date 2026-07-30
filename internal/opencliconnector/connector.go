package opencliconnector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tokhub/internal/browserconnector"
)

const MinimumOpenCLIVersion = "1.8.6"

type Config struct {
	ServerURL   string `json:"serverUrl"`
	ConnectorID string `json:"connectorId"`
	DeviceToken string `json:"deviceToken"`
}

type CommandRunner func(ctx context.Context, command string, args ...string) ([]byte, error)

type Executor struct {
	Binary string
	Run    CommandRunner
}

type RemoteTask struct {
	ID         string         `json:"id"`
	Provider   string         `json:"provider"`
	Action     string         `json:"action"`
	Request    map[string]any `json:"request"`
	LeaseToken string         `json:"leaseToken"`
}

type Client struct {
	Config     Config
	HTTPClient *http.Client
}

func ValidateServerURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("TokHub server URL is invalid")
	}
	hostname := strings.ToLower(parsed.Hostname())
	local := hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" || net.ParseIP(hostname).IsLoopback()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && local) {
		return "", errors.New("TokHub server URL must use HTTPS; local addresses may use HTTP")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func SaveConfig(path string, cfg Config) error {
	serverURL, err := ValidateServerURL(cfg.ServerURL)
	if err != nil {
		return err
	}
	cfg.ServerURL = serverURL
	if strings.TrimSpace(cfg.ConnectorID) == "" || len(strings.TrimSpace(cfg.DeviceToken)) < 32 {
		return errors.New("connector ID or device token is invalid")
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(parent, ".opencli-connector-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func LoadConfig(path string) (Config, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Config{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Config{}, errors.New("connector config must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Config{}, errors.New("connector config permissions must allow only the current user")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	serverURL, err := ValidateServerURL(cfg.ServerURL)
	if err != nil {
		return Config{}, err
	}
	cfg.ServerURL = serverURL
	if strings.TrimSpace(cfg.ConnectorID) == "" || len(strings.TrimSpace(cfg.DeviceToken)) < 32 {
		return Config{}, errors.New("connector config is incomplete")
	}
	return cfg, nil
}

func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "TokHub", "opencli-connector.json"), nil
}

func (e Executor) Execute(ctx context.Context, task browserconnector.Task) browserconnector.Result {
	args, err := browserconnector.BuildOpenCLICommand(task)
	if err != nil {
		return browserconnector.Result{OK: false, ErrorCode: "task_rejected", ErrorMessage: err.Error()}
	}
	binary := strings.TrimSpace(e.Binary)
	if binary == "" {
		binary = "opencli"
	}
	run := e.Run
	if run == nil {
		run = runOpenCLITaskCommand
	}
	raw, runErr := run(ctx, binary, args...)
	result, normalizeErr := browserconnector.NormalizeOpenCLIResult(task.Action, raw)
	if normalizeErr != nil {
		if runErr != nil {
			return browserconnector.NormalizeOpenCLICommandFailure(raw)
		}
		return browserconnector.Result{OK: false, ErrorCode: "opencli_command_failed", ErrorMessage: normalizeErr.Error()}
	}
	if runErr != nil && result.OK {
		return browserconnector.Result{
			OK: false, ErrorCode: "opencli_command_failed",
			ErrorMessage: "OpenCLI command failed; confirm Chrome and the OpenCLI extension are connected",
		}
	}
	return result
}

func runOpenCLITaskCommand(ctx context.Context, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stderr.Bytes(), err
	}
	return stdout.Bytes(), nil
}

func (e Executor) Version(ctx context.Context) (string, error) {
	binary := strings.TrimSpace(e.Binary)
	if binary == "" {
		binary = "opencli"
	}
	run := e.Run
	if run == nil {
		run = func(ctx context.Context, command string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, command, args...).Output()
		}
	}
	raw, err := run(ctx, binary, "--version")
	if err != nil {
		return "", errors.New("OpenCLI is unavailable")
	}
	version := versionPattern.FindString(string(raw))
	if version == "" {
		return "", errors.New("OpenCLI version could not be detected")
	}
	if compareVersions(version, MinimumOpenCLIVersion) < 0 {
		return version, fmt.Errorf("OpenCLI %s or newer is required", MinimumOpenCLIVersion)
	}
	return version, nil
}

func (e Executor) Doctor(ctx context.Context) error {
	binary := strings.TrimSpace(e.Binary)
	if binary == "" {
		binary = "opencli"
	}
	run := e.Run
	if run == nil {
		run = func(ctx context.Context, command string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, command, args...).Output()
		}
	}
	raw, err := run(ctx, binary, "doctor")
	diagnostic := strings.ToLower(string(raw))
	healthyExtension := strings.Contains(diagnostic, "extension: connected")
	healthyConnectivity := strings.Contains(diagnostic, "connectivity: passed")
	if err != nil ||
		strings.Contains(diagnostic, "[fail]") ||
		strings.Contains(diagnostic, "[missing] extension") ||
		strings.Contains(diagnostic, "extension: not connected") ||
		strings.Contains(diagnostic, "browser profile") && strings.Contains(diagnostic, "not connected") ||
		!healthyExtension ||
		!healthyConnectivity {
		return errors.New("OpenCLI browser check failed; run opencli doctor and confirm Chrome Bridge is connected")
	}
	return nil
}

func (c Client) Pair(ctx context.Context, serverURL string, pairingCode string) (Config, error) {
	normalized, err := ValidateServerURL(serverURL)
	if err != nil {
		return Config{}, err
	}
	var response struct {
		Connector struct {
			ID string `json:"id"`
		} `json:"connector"`
		DeviceToken string `json:"deviceToken"`
	}
	if err := c.doJSON(ctx, http.MethodPost, normalized+"/api/ai-browser-connectors/pair", "", map[string]string{
		"pairingCode": strings.TrimSpace(pairingCode),
	}, &response); err != nil {
		return Config{}, err
	}
	cfg := Config{ServerURL: normalized, ConnectorID: response.Connector.ID, DeviceToken: response.DeviceToken}
	if cfg.ConnectorID == "" || len(cfg.DeviceToken) < 32 {
		return Config{}, errors.New("TokHub pairing response is incomplete")
	}
	return cfg, nil
}

func (c Client) Heartbeat(ctx context.Context, opencliVersion string) error {
	return c.doJSON(ctx, http.MethodPost, c.Config.ServerURL+"/api/ai-browser-connectors/heartbeat", c.Config.DeviceToken, map[string]any{
		"opencliVersion": opencliVersion,
		"capabilities":   []string{"openai", "gemini", "deepseek"},
	}, nil)
}

func (c Client) Claim(ctx context.Context) (*RemoteTask, error) {
	var response struct {
		Task RemoteTask `json:"task"`
	}
	status, err := c.doJSONStatus(ctx, http.MethodPost, c.Config.ServerURL+"/api/ai-browser-connectors/tasks/claim", c.Config.DeviceToken, map[string]any{}, &response)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	return &response.Task, nil
}

func (c Client) Complete(ctx context.Context, taskID string, leaseToken string, result browserconnector.Result) error {
	return c.doJSON(ctx, http.MethodPost, c.Config.ServerURL+"/api/ai-browser-connectors/tasks/"+url.PathEscape(taskID)+"/complete", c.Config.DeviceToken, map[string]any{
		"leaseToken": leaseToken,
		"result":     result,
	}, nil)
}

func (c Client) doJSON(ctx context.Context, method string, endpoint string, token string, request any, response any) error {
	_, err := c.doJSONStatus(ctx, method, endpoint, token, request, response)
	return err
}

func (c Client) doJSONStatus(ctx context.Context, method string, endpoint string, token string, request any, response any) (int, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return 0, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(next *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				origin := via[0].URL
				if !strings.EqualFold(next.URL.Scheme, origin.Scheme) || !strings.EqualFold(next.URL.Host, origin.Host) {
					return http.ErrUseLastResponse
				}
				return nil
			},
		}
	}
	httpResponse, err := client.Do(httpRequest)
	if err != nil {
		return 0, err
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode == http.StatusNoContent {
		return httpResponse.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, 1<<20))
	if err != nil {
		return httpResponse.StatusCode, err
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return httpResponse.StatusCode, fmt.Errorf("TokHub request failed with status %d", httpResponse.StatusCode)
	}
	if response != nil && len(body) > 0 {
		if err := json.Unmarshal(body, response); err != nil {
			return httpResponse.StatusCode, err
		}
	}
	return httpResponse.StatusCode, nil
}

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

func compareVersions(left string, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := 0; index < 3; index++ {
		leftValue, _ := strconv.Atoi(leftParts[index])
		rightValue, _ := strconv.Atoi(rightParts[index])
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}
