package clientconnector

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const grokDeviceIdentityFile = "grok-device-identity"

func EnsureGrokDeviceIdentity() (string, error) {
	path := filepath.Join(connectorStateHome(), grokDeviceIdentityFile)
	if raw, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(raw))
		if len(value) >= 32 && len(value) <= 128 {
			return "device:" + value, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return RotateGrokDeviceIdentity()
}

func RotateGrokDeviceIdentity() (string, error) {
	home := connectorStateHome()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(buffer)
	path := filepath.Join(home, grokDeviceIdentityFile)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(value+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	return "device:" + value, nil
}

func RemoveGrokDeviceIdentity() error {
	err := os.Remove(filepath.Join(connectorStateHome(), grokDeviceIdentityFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func connectorStateHome() string {
	if value := strings.TrimSpace(os.Getenv("TOKHUB_CLIENT_CONNECTOR_HOME")); value != "" {
		return value
	}
	return "/data/connector"
}
