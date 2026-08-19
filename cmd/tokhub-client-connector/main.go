package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"tokhub/internal/buildinfo"
	"tokhub/internal/clientconnector"
	"tokhub/internal/officialconnector"
)

const officialClientSessionFileRetention = 24 * time.Hour

func main() {
	logger := log.New(os.Stdout, "TokHub Official Connector: ", 0)
	if err := run(os.Args[1:], logger); err != nil {
		logger.Printf("%v", err)
		os.Exit(1)
	}
}

func run(args []string, logger *log.Logger) error {
	if len(args) == 0 {
		printUsage(logger)
		return errors.New("command is required")
	}
	configPath := officialconnector.DefaultConfigPath()
	switch args[0] {
	case "help", "--help", "-h":
		printUsage(logger)
		return nil
	case "version", "--version":
		logger.Printf("TokHub Official Client Connector %s", buildinfo.Version)
		return nil
	case "pair":
		flags := flag.NewFlagSet("pair", flag.ContinueOnError)
		serverURL := flags.String("server", "", "TokHub server URL")
		pairingCode := flags.String("code", "", "one-time pairing code")
		flags.StringVar(&configPath, "config", configPath, "connector config path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*serverURL) == "" || strings.TrimSpace(*pairingCode) == "" {
			return errors.New("pair requires --server and --code")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cfg, err := officialconnector.Pair(ctx, *serverURL, *pairingCode)
		if err != nil {
			return fmt.Errorf("pairing failed: %w", err)
		}
		if err := officialconnector.SaveConfig(configPath, cfg); err != nil {
			return fmt.Errorf("save connector configuration: %w", err)
		}
		logger.Printf("Paired. Run login chatgpt or login grok, then run the connector.")
		return nil
	case "doctor":
		flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
		flags.StringVar(&configPath, "config", configPath, "connector config path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return doctor(configPath, logger)
	case "login":
		if len(args) < 2 {
			return errors.New("login requires chatgpt or grok")
		}
		return login(args[1])
	case "logout":
		provider := "all"
		if len(args) > 1 {
			provider = args[1]
		}
		return logout(provider)
	case "sessions":
		if len(args) < 2 || args[1] != "clean" {
			return errors.New("sessions supports the clean command")
		}
		removed, err := cleanOfficialClientSessions(time.Now(), officialClientSessionFileRetention)
		if err != nil {
			return err
		}
		logger.Printf("Removed %d expired official client session files.", removed)
		return nil
	case "run":
		flags := flag.NewFlagSet("run", flag.ContinueOnError)
		flags.StringVar(&configPath, "config", configPath, "connector config path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		cfg, err := officialconnector.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("pair this connector first: %w", err)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runConnector(ctx, officialconnector.Client{Config: cfg}, logger)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(logger *log.Logger) {
	logger.Print(`Usage:
  tokhub-client-connector pair --server <TokHub URL> --code <one-time code>
  tokhub-client-connector login chatgpt|grok
  tokhub-client-connector doctor
  tokhub-client-connector run
  tokhub-client-connector logout [chatgpt|grok|all]
  tokhub-client-connector sessions clean

The connector is designed for the TokHub read-only container. It delegates text-only turns to
Codex app-server or Grok Build ACP and denies tool, file, command, MCP, and web capabilities.`)
}

func doctor(configPath string, logger *log.Logger) error {
	if !insideContainer() {
		if _, err := exec.LookPath("docker"); err != nil {
			if _, podmanErr := exec.LookPath("podman"); podmanErr != nil {
				return errors.New("Docker or Podman is required")
			}
		}
		logger.Printf("Container runtime is available. Run doctor inside the pinned connector image for client checks.")
		return nil
	}
	codexVersion, codexErr := commandVersion("codex", "--version")
	grokVersion, grokErr := commandVersion("grok", "--version")
	if codexErr != nil && grokErr != nil {
		return errors.New("Codex and Grok official clients are unavailable")
	}
	logger.Printf("Container isolation detected; Codex=%s Grok=%s", nonEmpty(codexVersion, "unavailable"), nonEmpty(grokVersion, "unavailable"))
	cfg, err := officialconnector.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("connector is not paired: %w", err)
	}
	heartbeat := inspectClients(context.Background(), codexVersion, grokVersion)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := (officialconnector.Client{Config: cfg}).Heartbeat(ctx, heartbeat); err != nil {
		return fmt.Errorf("TokHub heartbeat failed: %w", err)
	}
	logger.Printf("TokHub signature, replay protection, and heartbeat checks passed")
	return nil
}

func login(provider string) error {
	command, args, normalized, err := officialLoginCommand(provider)
	if err != nil {
		return err
	}
	if err := runInteractive(command, args...); err != nil {
		return err
	}
	if normalized == "grok" {
		_, err := clientconnector.RotateGrokDeviceIdentity()
		return err
	}
	return nil
}

func officialLoginCommand(provider string) (string, []string, string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "chatgpt", "codex":
		return "codex", []string{"login", "--device-auth"}, "chatgpt", nil
	case "grok":
		return "grok", []string{"login", "--device-auth"}, "grok", nil
	default:
		return "", nil, "", errors.New("login supports chatgpt or grok")
	}
}

func logout(provider string) error {
	providers := []string{strings.ToLower(strings.TrimSpace(provider))}
	if providers[0] == "all" {
		providers = []string{"chatgpt", "grok"}
	}
	var lastErr error
	for _, item := range providers {
		switch item {
		case "chatgpt", "codex":
			if err := runInteractive("codex", "logout"); err != nil {
				lastErr = err
			}
			if err := clearOfficialClientSessions("chatgpt"); err != nil {
				lastErr = err
			}
		case "grok":
			if err := runInteractive("grok", "logout"); err != nil {
				lastErr = err
			} else if err := clientconnector.RemoveGrokDeviceIdentity(); err != nil {
				lastErr = err
			}
			if err := clearOfficialClientSessions("grok"); err != nil {
				lastErr = err
			}
		default:
			return errors.New("logout supports chatgpt, grok, or all")
		}
	}
	return lastErr
}

func clearOfficialClientSessions(provider string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var root string
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "chatgpt", "codex":
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			codexHome = filepath.Join(home, ".codex")
		}
		root = filepath.Join(codexHome, "sessions")
	case "grok":
		root = filepath.Join(home, ".grok", "sessions")
	default:
		return errors.New("session cleanup supports chatgpt or grok")
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	return os.MkdirAll(root, 0o700)
}

func runConnector(ctx context.Context, client officialconnector.Client, logger *log.Logger) error {
	codexVersion, _ := commandVersion("codex", "--version")
	grokVersion, _ := commandVersion("grok", "--version")
	heartbeat := inspectClients(ctx, codexVersion, grokVersion)
	heartbeat.ConnectorVersion = buildinfo.Version
	initialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err := client.Heartbeat(initialCtx, heartbeat)
	cancel()
	if err != nil {
		return fmt.Errorf("initial TokHub heartbeat failed: %w", err)
	}
	logger.Printf("Connected. One account, one task, and one execution loop are active.")
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()
	identityTicker := time.NewTicker(5 * time.Minute)
	defer identityTicker.Stop()
	sessionCleanupTicker := time.NewTicker(24 * time.Hour)
	defer sessionCleanupTicker.Stop()
	pollTicker := time.NewTicker(time.Second)
	defer pollTicker.Stop()
	if _, err := cleanOfficialClientSessions(time.Now(), officialClientSessionFileRetention); err != nil {
		logger.Printf("Session retention cleanup failed safely: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-identityTicker.C:
			heartbeat = inspectClients(ctx, codexVersion, grokVersion)
			heartbeat.ConnectorVersion = buildinfo.Version
		case <-sessionCleanupTicker.C:
			if _, err := cleanOfficialClientSessions(time.Now(), officialClientSessionFileRetention); err != nil {
				logger.Printf("Session retention cleanup failed safely: %v", err)
			}
		case <-heartbeatTicker.C:
			heartbeatCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := client.Heartbeat(heartbeatCtx, heartbeat); err != nil && ctx.Err() == nil {
				logger.Printf("Heartbeat interrupted; the connector will keep checking.")
			}
			cancel()
		case <-pollTicker.C:
			if err := processOneTask(ctx, client); err != nil && ctx.Err() == nil {
				logger.Printf("Task failed safely: %v", err)
			}
		}
	}
}

func processOneTask(ctx context.Context, client officialconnector.Client) error {
	claimCtx, cancelClaim := context.WithTimeout(ctx, 10*time.Second)
	task, err := client.Claim(claimCtx)
	cancelClaim()
	if err != nil || task == nil {
		return err
	}
	driver := driverFor(task.Provider)
	if driver == nil {
		return completeFailure(ctx, client, task, "provider_unsupported", "Official client provider is unsupported")
	}
	timeout := time.Until(task.ExpiresAt) - 5*time.Second
	if timeout < 5*time.Second {
		return completeFailure(ctx, client, task, "task_expired", "Official client task expired before execution")
	}
	taskCtx, cancelTask := context.WithTimeout(ctx, timeout)
	leaseMonitorDone := make(chan struct{})
	go monitorTaskLease(taskCtx, cancelTask, client, task, leaseMonitorDone)
	var result clientconnector.TaskResult
	payload := task.Payload
	if strings.TrimSpace(payload.PreviousSessionRef) != "" {
		payload.PreviousSessionRef, err = officialconnector.OpenSessionReference(client.Config, payload.PreviousSessionRef)
		if err != nil {
			cancelTask()
			close(leaseMonitorDone)
			return completeFailure(ctx, client, task, "session_expired", "Official client session belongs to another device or has expired")
		}
	}
	switch task.Action {
	case clientconnector.ActionIdentify:
		result = driver.Identify(taskCtx)
	case clientconnector.ActionGenerate:
		result = driver.Generate(taskCtx, payload)
	case clientconnector.ActionDeleteSession:
		result = driver.DeleteSession(taskCtx, payload.PreviousSessionRef)
	default:
		result = clientconnector.TaskResult{ErrorCode: "action_unsupported", ErrorMessage: "Official client action is unsupported"}
	}
	if result.OK && strings.TrimSpace(result.SessionRef) != "" {
		result.SessionRef, err = officialconnector.SealSessionReference(client.Config, result.SessionRef)
		if err != nil {
			result = clientconnector.TaskResult{ErrorCode: "session_seal_failed", ErrorMessage: "Official client session could not be device sealed"}
		}
	}
	cancelTask()
	close(leaseMonitorDone)
	completeCtx, cancelComplete := context.WithTimeout(ctx, 10*time.Second)
	defer cancelComplete()
	return client.Complete(completeCtx, task.ID, task.LeaseToken, result)
}

func monitorTaskLease(ctx context.Context, cancel context.CancelFunc, client officialconnector.Client, task *clientconnector.ClaimedTask, done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
			active, err := client.TaskActive(checkCtx, task.ID, task.LeaseToken)
			checkCancel()
			if err != nil || !active {
				cancel()
				return
			}
		}
	}
}

func completeFailure(ctx context.Context, client officialconnector.Client, task *clientconnector.ClaimedTask, code, message string) error {
	completeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return client.Complete(completeCtx, task.ID, task.LeaseToken, clientconnector.TaskResult{ErrorCode: code, ErrorMessage: message})
}

func driverFor(provider string) clientconnector.Driver {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return clientconnector.CodexDriver{}
	case "grok":
		return clientconnector.GrokDriver{}
	default:
		return nil
	}
}

func inspectClients(ctx context.Context, codexVersion, grokVersion string) clientconnector.Heartbeat {
	heartbeat := clientconnector.Heartbeat{
		ConnectorVersion: buildinfo.Version, CodexVersion: codexVersion, GrokVersion: grokVersion,
		Capabilities: []string{}, Identity: map[string]clientconnector.IdentityStatus{},
	}
	if codexVersion != "" {
		heartbeat.Capabilities = append(heartbeat.Capabilities, "chatgpt")
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		result := (clientconnector.CodexDriver{}).Identify(checkCtx)
		cancel()
		heartbeat.Identity["openai"] = clientconnector.IdentityStatus{LoggedIn: result.OK, AccountMask: result.AccountMask, IdentityAssurance: result.IdentityAssurance, CheckedAt: time.Now()}
	}
	if grokVersion != "" {
		heartbeat.Capabilities = append(heartbeat.Capabilities, "grok")
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		result := (clientconnector.GrokDriver{}).Identify(checkCtx)
		cancel()
		heartbeat.Identity["grok"] = clientconnector.IdentityStatus{LoggedIn: result.OK, AccountMask: result.AccountMask, IdentityAssurance: result.IdentityAssurance, CheckedAt: time.Now()}
	}
	return heartbeat
}

func commandVersion(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func runInteractive(name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

func cleanOfficialClientSessions(now time.Time, retention time.Duration) (int, error) {
	if retention <= 0 {
		return 0, errors.New("session retention must be positive")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	roots := []string{filepath.Join(codexHome, "sessions"), filepath.Join(home, ".grok", "sessions")}
	cutoff := now.Add(-retention)
	removed := 0
	for _, root := range roots {
		directories := []string{}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if errors.Is(walkErr, os.ErrNotExist) {
					return nil
				}
				return walkErr
			}
			if entry.IsDir() {
				if path != root {
					directories = append(directories, path)
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				removed++
			}
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("clean %s: %w", root, err)
		}
		sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
		for _, directory := range directories {
			_ = os.Remove(directory)
		}
	}
	return removed, nil
}

func insideContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	return strings.TrimSpace(os.Getenv("TOKHUB_CONNECTOR_CONTAINER")) == "1"
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
