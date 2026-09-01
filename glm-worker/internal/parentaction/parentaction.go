package parentaction

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Prepared struct {
	Action string `json:"action"`
	Token  string `json:"token"`
	Path   string `json:"path"`
}

const (
	StageDirName    = ".glm-worker-parent-actions"
	placeholder     = "__GLM_PARENT_ACTION_PAYLOAD__\n"
	tokenHeaderKey  = "GLM_PARENT_ACTION_TOKEN:"
	maxPayloadBytes = 1 << 20
)

func Prepare(repoRoot, action string) (Prepared, error) {
	if !validPayloadAction(action) {
		return Prepared{}, fmt.Errorf("unsupported parent payload action %q", action)
	}
	stageDir := filepath.Join(repoRoot, StageDirName)
	if err := ensureStageDir(stageDir); err != nil {
		return Prepared{}, err
	}
	token, err := newToken()
	if err != nil {
		return Prepared{}, err
	}
	path := payloadPath(stageDir, action, token)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Prepared{}, fmt.Errorf("create parent action staging file: %w", err)
	}
	initial := tokenHeader(token) + placeholder
	if _, err := io.WriteString(file, initial); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return Prepared{}, fmt.Errorf("initialize parent action staging file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return Prepared{}, fmt.Errorf("close parent action staging file: %w", err)
	}
	return Prepared{Action: action, Token: token, Path: path}, nil
}

func Consume(repoRoot, action, token string) ([]byte, error) {
	if !validPayloadAction(action) {
		return nil, fmt.Errorf("unsupported parent payload action %q", action)
	}
	if !validToken(token) {
		return nil, fmt.Errorf("invalid parent action token")
	}
	stageDir := filepath.Join(repoRoot, StageDirName)
	if err := validateStageDir(stageDir); err != nil {
		return nil, err
	}
	path := payloadPath(stageDir, action, token)
	raw, err := readRegularPayload(path, len(tokenHeader(token)))
	if err != nil {
		return nil, err
	}
	payload, err := decodePayload(raw, token)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil {
		return nil, fmt.Errorf("consume parent action staging file: %w", err)
	}
	return payload, nil
}

func decodePayload(raw []byte, token string) ([]byte, error) {
	header := []byte(tokenHeader(token))
	if !bytes.HasPrefix(raw, header) {
		return nil, fmt.Errorf("parent action staging token binding is invalid")
	}
	payload := raw[len(header):]
	if len(payload) > maxPayloadBytes {
		return nil, fmt.Errorf("parent action payload exceeds %d bytes", maxPayloadBytes)
	}
	if len(payload) == 0 || bytes.Contains(payload, []byte(placeholder)) {
		return nil, fmt.Errorf("parent action payload was not supplied completely")
	}
	return payload, nil
}

func readRegularPayload(path string, headerBytes int) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("parent action staging file unavailable: %w", err)
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("parent action staging path is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open parent action staging file: %w", err)
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat parent action staging file: %w", err)
	}
	if !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("parent action staging file changed while opening")
	}
	limit := int64(maxPayloadBytes + headerBytes + 1)
	payload, readErr := io.ReadAll(io.LimitReader(file, limit))
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read parent action staging file: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close parent action staging file: %w", closeErr)
	}
	return payload, nil
}

func ensureStageDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create parent action staging directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect parent action staging directory: %w", err)
	}
	return validateStageDirInfo(info)
}

func validateStageDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect parent action staging directory: %w", err)
	}
	return validateStageDirInfo(info)
}

func validateStageDirInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("parent action staging directory is not a real directory")
	}
	return nil
}

func tokenHeader(token string) string {
	return tokenHeaderKey + token + "\n"
}

func payloadPath(stageDir, action, token string) string {
	return filepath.Join(stageDir, action+"-"+token+".txt")
}

func validPayloadAction(action string) bool {
	_, ok := LookupPayloadAction(action)
	return ok
}

func newToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate parent action token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func validToken(token string) bool {
	if len(token) != 32 || strings.ToLower(token) != token {
		return false
	}
	decoded, err := hex.DecodeString(token)
	return err == nil && len(decoded) == 16
}
