package opencliconnector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tokhub/internal/browserconnector"
)

func TestValidateServerURLRequiresHTTPSOrLocalHTTP(t *testing.T) {
	for _, value := range []string{
		"https://tokhub.example.com",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
	} {
		if _, err := ValidateServerURL(value); err != nil {
			t.Fatalf("%q was rejected: %v", value, err)
		}
	}
	for _, value := range []string{
		"http://tokhub.example.com",
		"file:///private/fixture",
		"https://user:password@tokhub.example.com",
	} {
		if normalized, err := ValidateServerURL(value); err == nil {
			t.Fatalf("unsafe URL %q was accepted as %q", value, normalized)
		}
	}
}

func TestExecutorUsesAllowlistedOpenCLIArguments(t *testing.T) {
	var binary string
	var args []string
	executor := Executor{
		Binary: "opencli-test",
		Run: func(_ context.Context, command string, commandArgs ...string) ([]byte, error) {
			binary = command
			args = append([]string(nil), commandArgs...)
			return []byte(`{"success":true,"data":{"content":"回答"}}`), nil
		},
	}
	result := executor.Execute(context.Background(), browserconnector.Task{
		Provider: "deepseek", Action: browserconnector.ActionAsk, Prompt: "你好; rm -rf /",
	})
	if !result.OK || result.Content != "回答" {
		t.Fatalf("execution result = %#v", result)
	}
	if binary != "opencli-test" {
		t.Fatalf("binary = %q", binary)
	}
	want := []string{"deepseek", "ask", "你好; rm -rf /", "-f", "json", "--timeout", "90", "--new", "true"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

func TestExecutorClassifiesOpenCLIFailureWithoutLeakingProviderDetails(t *testing.T) {
	executor := Executor{
		Run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("error:\n  code: AUTH_REQUIRED\n  message: session cookie missing; token=sensitive"), errors.New("exit status 77")
		},
	}
	result := executor.Execute(context.Background(), browserconnector.Task{
		Provider: "deepseek", Action: browserconnector.ActionStatus,
	})
	if result.OK || result.ErrorCode != "login_required" ||
		strings.Contains(result.ErrorMessage, "sensitive") ||
		strings.Contains(result.ErrorMessage, "cookie") {
		t.Fatalf("execution failure was not safely classified: %#v", result)
	}
}

func TestExecutorDoctorRequiresConnectedBrowserBridge(t *testing.T) {
	tests := []struct {
		name   string
		output string
		runErr error
	}{
		{name: "non-zero command", runErr: errors.New("browser disconnected; token=sensitive")},
		{name: "zero exit with failed diagnostic", output: "[MISSING] Extension: not connected\n[FAIL] Connectivity: failed"},
		{name: "zero exit with disconnected profile", output: `[OK] Daemon\nBrowser profile "work" is not connected`},
		{name: "zero exit without healthy evidence", output: "OpenCLI doctor finished"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var args []string
			executor := Executor{
				Binary: "opencli-test",
				Run: func(_ context.Context, _ string, commandArgs ...string) ([]byte, error) {
					args = append([]string(nil), commandArgs...)
					return []byte(test.output), test.runErr
				},
			}
			err := executor.Doctor(context.Background())
			if err == nil || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("Doctor() error = %v", err)
			}
			if !reflect.DeepEqual(args, []string{"doctor"}) {
				t.Fatalf("doctor args = %#v", args)
			}
		})
	}
}

func TestExecutorDoctorAcceptsHealthyBridge(t *testing.T) {
	executor := Executor{
		Run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("[OK] Daemon: running\n[OK] Extension: connected\n[OK] Connectivity: passed"), nil
		},
	}
	if err := executor.Doctor(context.Background()); err != nil {
		t.Fatalf("Doctor() rejected a healthy browser bridge: %v", err)
	}
}

func TestExecutorVersionEnforcesOpenCLICompatibilityFloor(t *testing.T) {
	for _, test := range []struct {
		version string
		ok      bool
	}{
		{version: "1.8.4", ok: false},
		{version: MinimumOpenCLIVersion, ok: true},
		{version: "1.9.0", ok: true},
	} {
		executor := Executor{
			Run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
				return []byte("opencli v" + test.version), nil
			},
		}
		version, err := executor.Version(context.Background())
		if test.ok && (err != nil || version != test.version) {
			t.Fatalf("Version() for %s = %q, %v", test.version, version, err)
		}
		if !test.ok && err == nil {
			t.Fatalf("Version() accepted unsupported OpenCLI %s", test.version)
		}
	}
}

func TestConfigFileUsesOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connector.json")
	cfg := Config{
		ServerURL:   "https://tokhub.example.com",
		ConnectorID: "aibc_test",
		DeviceToken: "secret-device-token-with-at-least-forty-characters",
	}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, cfg) {
		t.Fatalf("loaded config = %#v, want %#v", loaded, cfg)
	}
}

func TestSaveConfigReplacesSymlinkWithoutOverwritingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	path := filepath.Join(dir, "connector.json")
	if err := os.WriteFile(target, []byte("preserve-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		ServerURL:   "https://tokhub.example.com",
		ConnectorID: "aibc_test",
		DeviceToken: "secret-device-token-with-at-least-forty-characters",
	}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	targetRaw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(targetRaw) != "preserve-me" {
		t.Fatalf("symlink target was overwritten: %q", targetRaw)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("config path remained a symlink")
	}
}

func TestLoadConfigRejectsBroadPermissionsAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		ServerURL:   "https://tokhub.example.com",
		ConnectorID: "aibc_test",
		DeviceToken: "secret-device-token-with-at-least-forty-characters",
	}
	path := filepath.Join(dir, "connector.json")
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("world-readable connector config was accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "connector-link.json")
	if err := os.Symlink(path, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(symlink); err == nil {
		t.Fatal("symlink connector config was accepted")
	}
}
