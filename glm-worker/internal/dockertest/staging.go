package dockertest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type StagingRequest struct {
	SchemaVersion int      `json:"schema_version"`
	Token         string   `json:"token"`
	Args          []string `json:"args"`
}

type StagingResponse struct {
	SchemaVersion int    `json:"schema_version"`
	Token         string `json:"token"`
	ExitCode      int    `json:"exit_code"`
	Stdout        string `json:"stdout"`
	Stderr        string `json:"stderr"`
	Failure       string `json:"failure,omitempty"`
}

type StagingClient struct {
	StagingDir    string
	ResponseDir   string
	Timeout       time.Duration
	PollInterval  time.Duration
	generateToken func() (string, error)
}

const StagingSchemaVersion = 1

const StagingRequestTimeoutDefault = 60 * time.Minute

const StagingClientTimeoutDefault = StagingRequestTimeoutDefault + 5*time.Minute

const StagingClientPollIntervalDefault = 250 * time.Millisecond

const StagingMaxResponseBytes = 64 << 20

const stagingTokenCharset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ._-"

const (
	stagingRequestPrefix  = "request-"
	stagingResponsePrefix = "response-"
	stagingFileSuffix     = ".json"
	stagingMaxTokenLength = 128
)

func StagingRequestFileName(token string) string {
	return stagingRequestPrefix + token + stagingFileSuffix
}

func StagingResponseFileName(token string) string {
	return stagingResponsePrefix + token + stagingFileSuffix
}

func IsStagingRequestFileName(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	return strings.HasPrefix(name, stagingRequestPrefix) && strings.HasSuffix(name, stagingFileSuffix)
}

func StagingTokenFromRequestFileName(name string) (string, bool) {
	if !strings.HasPrefix(name, stagingRequestPrefix) || !strings.HasSuffix(name, stagingFileSuffix) {
		return "", false
	}
	token := strings.TrimSuffix(strings.TrimPrefix(name, stagingRequestPrefix), stagingFileSuffix)
	return token, validStagingToken(token)
}

func validStagingToken(token string) bool {
	if token == "" || len(token) > stagingMaxTokenLength || token == "." || token == ".." {
		return false
	}
	for _, char := range token {
		if !strings.ContainsRune(stagingTokenCharset, char) {
			return false
		}
	}
	return true
}

func ParseStagingRequest(data []byte) (StagingRequest, error) {
	var request StagingRequest
	if err := decodeStrictStagingJSON(data, &request); err != nil {
		return StagingRequest{}, fmt.Errorf("staging requestの形式が不正です: %w", err)
	}
	if err := request.Validate(); err != nil {
		return StagingRequest{}, err
	}
	return request, nil
}

func ParseStagingResponse(data []byte) (StagingResponse, error) {
	var response StagingResponse
	if err := decodeStrictStagingJSON(data, &response); err != nil {
		return StagingResponse{}, fmt.Errorf("staging responseの形式が不正です: %w", err)
	}
	if response.SchemaVersion != StagingSchemaVersion {
		return StagingResponse{}, fmt.Errorf("staging responseのschema_versionが不正です: %d", response.SchemaVersion)
	}
	return response, nil
}

func decodeStrictStagingJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("余分なJSON値があります")
		}
		return fmt.Errorf("JSON文書の末尾が不正です: %w", err)
	}
	return nil
}

func (r StagingRequest) Validate() error {
	if r.SchemaVersion != StagingSchemaVersion {
		return fmt.Errorf("staging requestのschema_versionが不正です: %d", r.SchemaVersion)
	}
	if !validStagingToken(r.Token) {
		return fmt.Errorf("staging requestのtokenが不正です")
	}
	if len(r.Args) == 0 {
		return fmt.Errorf("staging requestのargsが空です")
	}
	for _, arg := range r.Args {
		if arg == "" {
			return fmt.Errorf("staging requestのargsに空要素があります")
		}
		if arg == "--" {
			return fmt.Errorf("staging requestのargsに終端記号があります")
		}
	}
	return nil
}

func (c *StagingClient) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	token, err := c.token()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "goquality test:", err)
		return 1
	}
	request := StagingRequest{SchemaVersion: StagingSchemaVersion, Token: token, Args: args}
	if err := request.Validate(); err != nil {
		_, _ = fmt.Fprintln(stderr, "goquality test:", err)
		return 1
	}
	if err := c.writeRequest(request); err != nil {
		_, _ = fmt.Fprintln(stderr, "goquality test:", err)
		return 1
	}
	response, ok := c.awaitResponse(ctx, request, stderr)
	if !ok {
		c.removeRequest(request.Token)
		return 1
	}
	_, _ = io.WriteString(stdout, response.Stdout)
	_, _ = io.WriteString(stderr, response.Stderr)
	if response.Failure != "" {
		_, _ = fmt.Fprintln(stderr, "goquality test:", response.Failure)
	}
	return response.ExitCode
}

func (c *StagingClient) writeRequest(request StagingRequest) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	final := filepath.Join(c.StagingDir, StagingRequestFileName(request.Token))
	responsePath := filepath.Join(c.ResponseDir, StagingResponseFileName(request.Token))
	if _, err := os.Lstat(responsePath); err == nil {
		return fmt.Errorf("staging requestのtokenが重複しています: %s", request.Token)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("staging responseの確認に失敗しました: %w", err)
	}
	temp := filepath.Join(c.StagingDir, ".request-"+request.Token+".tmp")
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return fmt.Errorf("staging requestのtokenが重複しています: %s", request.Token)
	}
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.Remove(temp)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(temp, final); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("staging requestのtokenが重複しています: %s", request.Token)
		}
		return err
	}
	published = true
	return nil
}

func (c *StagingClient) awaitResponse(ctx context.Context, request StagingRequest, stderr io.Writer) (StagingResponse, bool) {
	path := filepath.Join(c.ResponseDir, StagingResponseFileName(request.Token))
	deadline := time.Now().Add(c.timeout())
	ticker := time.NewTicker(c.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(stderr, "goquality test: staged Docker診断を取消しました")
			return StagingResponse{}, false
		case <-ticker.C:
		}
		response, ok, readErr := readStagingResponse(path, request.Token)
		if readErr != nil {
			_, _ = fmt.Fprintln(stderr, "goquality test:", readErr)
			return StagingResponse{}, false
		}
		if ok {
			return response, true
		}
		if !time.Now().Before(deadline) {
			_, _ = fmt.Fprintln(stderr, "goquality test: staging responseが締切内に得られませんでした")
			return StagingResponse{}, false
		}
	}
}

func readStagingResponse(path, token string) (StagingResponse, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return StagingResponse{}, false, nil
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, StagingMaxResponseBytes+1))
	if err != nil {
		return StagingResponse{}, false, nil
	}
	if len(data) > StagingMaxResponseBytes {
		return StagingResponse{}, false, fmt.Errorf("staging responseが符号化後size上限を超えました")
	}
	response, err := ParseStagingResponse(data)
	if err != nil || response.Token != token {
		return StagingResponse{}, false, nil
	}
	return response, true, nil
}

func (c *StagingClient) removeRequest(token string) {
	_ = os.Remove(filepath.Join(c.StagingDir, StagingRequestFileName(token)))
}

func (c *StagingClient) token() (string, error) {
	if c.generateToken != nil {
		return c.generateToken()
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("staging request tokenを生成できません: %w", err)
	}
	return hex.EncodeToString(random[:]), nil
}

func (c *StagingClient) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return StagingClientTimeoutDefault
}

func (c *StagingClient) pollInterval() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return StagingClientPollIntervalDefault
}
