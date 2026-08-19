package officialconnector

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tokhub/internal/clientconnector"
)

type Config struct {
	ServerURL   string `json:"serverUrl"`
	DeviceToken string `json:"deviceToken"`
	PublicKey   string `json:"publicKey"`
	PrivateKey  string `json:"privateKey"`
}

type Client struct {
	Config Config
	HTTP   *http.Client
}

func DefaultConfigPath() string {
	if value := strings.TrimSpace(os.Getenv("TOKHUB_CLIENT_CONNECTOR_CONFIG")); value != "" {
		return value
	}
	home := strings.TrimSpace(os.Getenv("TOKHUB_CLIENT_CONNECTOR_HOME"))
	if home == "" {
		home = "/data/connector"
	}
	return filepath.Join(home, "config.json")
}

func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func SaveConfig(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func SealSessionReference(cfg Config, sessionRef string) (string, error) {
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return "", nil
	}
	gcm, err := sessionReferenceCipher(cfg)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(sessionRef), []byte(clientconnector.SealedSessionReferencePrefix))
	return clientconnector.SealedSessionReferencePrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func OpenSessionReference(cfg Config, sealedRef string) (string, error) {
	sealedRef = strings.TrimSpace(sealedRef)
	if sealedRef == "" {
		return "", nil
	}
	if !strings.HasPrefix(sealedRef, clientconnector.SealedSessionReferencePrefix) {
		return "", errors.New("official client session reference is not device sealed")
	}
	gcm, err := sessionReferenceCipher(cfg)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(sealedRef, clientconnector.SealedSessionReferencePrefix))
	if err != nil || len(raw) <= gcm.NonceSize() {
		return "", errors.New("official client session reference is invalid")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(clientconnector.SealedSessionReferencePrefix))
	if err != nil {
		return "", errors.New("official client session reference cannot be opened by this device")
	}
	return string(plain), nil
}

func sessionReferenceCipher(cfg Config) (cipher.AEAD, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	privateKey, _ := base64.RawURLEncoding.DecodeString(cfg.PrivateKey)
	material := append([]byte("tokhub-official-client-session-reference-v1\x00"), privateKey...)
	key := sha256.Sum256(material)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (c Config) Validate() error {
	server, err := validateServerURL(c.ServerURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.DeviceToken) == "" || strings.TrimSpace(c.PublicKey) == "" || strings.TrimSpace(c.PrivateKey) == "" {
		return errors.New("official client connector configuration is incomplete")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(c.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("official client public key is invalid")
	}
	privateKey, err := base64.RawURLEncoding.DecodeString(c.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("official client private key is invalid")
	}
	if !bytes.Equal(privateKey[32:], publicKey) {
		return errors.New("official client key pair does not match")
	}
	_ = server
	return nil
}

func Pair(ctx context.Context, serverURL, pairingCode string) (Config, error) {
	server, err := validateServerURL(serverURL)
	if err != nil {
		return Config{}, err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Config{}, err
	}
	request := map[string]string{
		"pairingCode": strings.TrimSpace(pairingCode),
		"publicKey":   base64.RawURLEncoding.EncodeToString(publicKey),
	}
	raw, _ := json.Marshal(request)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/api/ai-client-connectors/pair", bytes.NewReader(raw))
	if err != nil {
		return Config{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(httpRequest)
	if err != nil {
		return Config{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Config{}, responseError(response)
	}
	var result struct {
		DeviceToken string `json:"deviceToken"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(&result); err != nil {
		return Config{}, err
	}
	cfg := Config{
		ServerURL: server, DeviceToken: result.DeviceToken,
		PublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey),
	}
	return cfg, cfg.Validate()
}

func (c Client) Heartbeat(ctx context.Context, heartbeat clientconnector.Heartbeat) error {
	return c.signedJSON(ctx, http.MethodPost, "/api/ai-client-connectors/heartbeat", heartbeat, nil)
}

func (c Client) Claim(ctx context.Context) (*clientconnector.ClaimedTask, error) {
	var response struct {
		Task clientconnector.ClaimedTask `json:"task"`
	}
	status, err := c.signedJSONStatus(ctx, http.MethodPost, "/api/ai-client-connectors/tasks/claim", map[string]any{}, &response)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	return &response.Task, nil
}

func (c Client) Complete(ctx context.Context, taskID, leaseToken string, result clientconnector.TaskResult) error {
	path := "/api/ai-client-connectors/tasks/" + url.PathEscape(taskID) + "/complete"
	return c.signedJSON(ctx, http.MethodPost, path, clientconnector.CompleteTaskRequest{LeaseToken: leaseToken, Result: result}, nil)
}

func (c Client) TaskActive(ctx context.Context, taskID, leaseToken string) (bool, error) {
	path := "/api/ai-client-connectors/tasks/" + url.PathEscape(taskID) + "/status"
	var response struct {
		Active bool `json:"active"`
	}
	err := c.signedJSON(ctx, http.MethodPost, path, map[string]string{"leaseToken": leaseToken}, &response)
	return response.Active, err
}

func (c Client) signedJSON(ctx context.Context, method, path string, requestBody, responseBody any) error {
	_, err := c.signedJSONStatus(ctx, method, path, requestBody, responseBody)
	return err
}

func (c Client) signedJSONStatus(ctx context.Context, method, path string, requestBody, responseBody any) (int, error) {
	if err := c.Config.Validate(); err != nil {
		return 0, err
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return 0, err
	}
	timestamp := time.Now().Unix()
	nonceBytes := make([]byte, 18)
	if _, err := rand.Read(nonceBytes); err != nil {
		return 0, err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	privateKey, _ := base64.RawURLEncoding.DecodeString(c.Config.PrivateKey)
	signature, err := clientconnector.SignRequest(ed25519.PrivateKey(privateKey), method, path, body, timestamp, nonce)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Config.ServerURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.Config.DeviceToken)
	request.Header.Set("X-TokHub-Timestamp", strconv.FormatInt(timestamp, 10))
	request.Header.Set("X-TokHub-Nonce", nonce)
	request.Header.Set("X-TokHub-Signature", signature)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 4 * time.Minute}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, responseError(response)
	}
	if responseBody != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 3<<20)).Decode(responseBody); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}

func validateServerURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("TokHub server URL is invalid")
	}
	if parsed.Scheme == "http" && !loopbackHost(parsed.Hostname()) {
		return "", errors.New("TokHub connector requires HTTPS; HTTP is limited to loopback development")
	}
	parsed.Path, parsed.RawQuery, parsed.Fragment = "", "", ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func loopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func responseError(response *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &envelope)
	message := strings.TrimSpace(envelope.Error.Message)
	if message == "" {
		message = strings.TrimSpace(string(raw))
	}
	return fmt.Errorf("TokHub returned HTTP %d: %s", response.StatusCode, message)
}
