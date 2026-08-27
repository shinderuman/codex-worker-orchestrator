package codexlimit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Window struct {
	UsedPercent        *int64  `json:"used_percent"`
	WindowDurationMins int64   `json:"window_duration_mins"`
	ResetsAt           *int64  `json:"resets_at"`
	ResetsAtRFC3339    *string `json:"resets_at_rfc3339"`
}

type Snapshot struct {
	FiveHour             Window  `json:"five_hour"`
	Weekly               *Window `json:"weekly"`
	PlanType             *string `json:"plan_type"`
	RateLimitReachedType *string `json:"rate_limit_reached_type"`
}

type limitWindow struct {
	UsedPercent        *int64 `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

type limitSnapshot struct {
	LimitID              string       `json:"limitId"`
	Primary              *limitWindow `json:"primary"`
	Secondary            *limitWindow `json:"secondary"`
	PlanType             *string      `json:"planType"`
	RateLimitReachedType *string      `json:"rateLimitReachedType"`
}

type rateLimitsResult struct {
	RateLimits          *limitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID map[string]*limitSnapshot `json:"rateLimitsByLimitId"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type initializeParams struct {
	ClientInfo clientInfo `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type rpcMessage struct {
	ID     *int64           `json:"id"`
	Method string           `json:"method"`
	Result *json.RawMessage `json:"result"`
	Error  *rpcError        `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

const (
	fiveHourWindowMins  = int64(300)
	weeklyWindowMins    = int64(10080)
	requestIDInitialize = int64(1)
	requestIDRateLimits = int64(2)
	appServerTimeout    = 30 * time.Second
	clientName          = "glm-worker"
	clientVersion       = "1"
	stderrTailLimit     = 500
)

var (
	ErrCodexBinaryNotFound     = errors.New("codex binary not found")
	ErrAppServerStart          = errors.New("codex app-server failed to start")
	ErrAppServerProtocol       = errors.New("codex app-server protocol failure")
	ErrAppServerTimeout        = errors.New("codex app-server response timed out")
	ErrRateLimitsRead          = errors.New("account/rateLimits/read returned no usable codex rate limits")
	ErrFiveHourWindowMissing   = errors.New("rate limits response has no 300-minute window")
	ErrWindowAmbiguous         = errors.New("rate limits response has duplicate windows for one duration")
	ErrFiveHourResetsAtMissing = errors.New("300-minute window has no resetsAt")
)

func Read(bin string) (Snapshot, error) {
	return ReadWithTimeout(bin, appServerTimeout)
}

func ReadWithTimeout(bin string, timeout time.Duration) (Snapshot, error) {
	if _, err := exec.LookPath(bin); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrCodexBinaryNotFound, bin)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: stdin pipe: %w", ErrAppServerStart, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: stdout pipe: %w", ErrAppServerStart, err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.WaitDelay = 3 * time.Second

	if err := cmd.Start(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrAppServerStart, err)
	}

	result, exchangeErr := exchange(ctx, stdin, stdout)
	killErr := cmd.Process.Kill()
	waitErr := cmd.Wait()
	if exchangeErr != nil {
		return Snapshot{}, withStderrTail(exchangeErr, stderr.String())
	}
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return Snapshot{}, fmt.Errorf("%w: kill: %w", ErrAppServerProtocol, killErr)
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return Snapshot{}, fmt.Errorf("%w: wait: %w", ErrAppServerProtocol, waitErr)
	}
	return buildSnapshot(result)
}

func exchange(ctx context.Context, stdin io.WriteCloser, stdout io.Reader) (*json.RawMessage, error) {
	encoder := json.NewEncoder(stdin)
	reader := bufio.NewReader(stdout)

	initialize := rpcRequest{JSONRPC: "2.0", ID: requestIDInitialize, Method: "initialize", Params: initializeParams{
		ClientInfo: clientInfo{Name: clientName, Title: clientName, Version: clientVersion},
	}}
	if err := encoder.Encode(initialize); err != nil {
		return nil, fmt.Errorf("%w: request write: %w", ErrAppServerProtocol, err)
	}
	initializeResult, err := awaitMessage(ctx, reader, requestIDInitialize)
	if err != nil {
		return nil, err
	}
	if initializeResult.Error != nil {
		return nil, fmt.Errorf("%w: initialize error %d: %s", ErrAppServerProtocol, initializeResult.Error.Code, initializeResult.Error.Message)
	}

	notification := rpcRequest{JSONRPC: "2.0", Method: "initialized"}
	if err := encoder.Encode(notification); err != nil {
		return nil, fmt.Errorf("%w: request write: %w", ErrAppServerProtocol, err)
	}
	read := rpcRequest{JSONRPC: "2.0", ID: requestIDRateLimits, Method: "account/rateLimits/read", Params: struct{}{}}
	if err := encoder.Encode(read); err != nil {
		return nil, fmt.Errorf("%w: request write: %w", ErrAppServerProtocol, err)
	}
	message, err := awaitMessage(ctx, reader, requestIDRateLimits)
	if err != nil {
		return nil, err
	}
	if message.Error != nil {
		return nil, fmt.Errorf("%w: error %d: %s", ErrRateLimitsRead, message.Error.Code, message.Error.Message)
	}
	if message.Result == nil {
		return nil, fmt.Errorf("%w: response has no result", ErrRateLimitsRead)
	}
	return message.Result, nil
}

func awaitMessage(ctx context.Context, reader *bufio.Reader, id int64) (rpcMessage, error) {
	for {
		message, err := readMessage(ctx, reader)
		if err != nil {
			return rpcMessage{}, err
		}
		if message.ID == nil || *message.ID != id {
			continue
		}
		return message, nil
	}
}

func readMessage(ctx context.Context, reader *bufio.Reader) (rpcMessage, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		if ctx.Err() != nil {
			return rpcMessage{}, fmt.Errorf("%w", ErrAppServerTimeout)
		}
		return rpcMessage{}, fmt.Errorf("%w: response stream ended: %w", ErrAppServerProtocol, err)
	}
	trimmed := strings.TrimSpace(line)
	var message rpcMessage
	if trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &message); err != nil {
			return rpcMessage{}, fmt.Errorf("%w: malformed line %q: %w", ErrAppServerProtocol, truncateLine(trimmed), err)
		}
	}
	return message, nil
}

func buildSnapshot(result *json.RawMessage) (Snapshot, error) {
	var decoded rateLimitsResult
	if err := json.Unmarshal(*result, &decoded); err != nil {
		return Snapshot{}, fmt.Errorf("%w: result decode: %w", ErrRateLimitsRead, err)
	}
	snapshot := decoded.RateLimits
	if snapshot == nil {
		snapshot = decoded.RateLimitsByLimitID["codex"]
	}
	if snapshot == nil {
		return Snapshot{}, fmt.Errorf("%w: result has no codex limit entry", ErrRateLimitsRead)
	}

	fiveHour, err := selectWindow(snapshot, fiveHourWindowMins)
	if err != nil {
		return Snapshot{}, err
	}
	if fiveHour.ResetsAt == nil {
		return Snapshot{}, ErrFiveHourResetsAtMissing
	}

	output := Snapshot{
		FiveHour:             toWindow(fiveHour, fiveHourWindowMins),
		PlanType:             snapshot.PlanType,
		RateLimitReachedType: snapshot.RateLimitReachedType,
	}
	weekly, err := selectWindow(snapshot, weeklyWindowMins)
	if err != nil {
		return Snapshot{}, err
	}
	if weekly != nil {
		window := toWindow(weekly, weeklyWindowMins)
		output.Weekly = &window
	}
	return output, nil
}

func selectWindow(snapshot *limitSnapshot, mins int64) (*limitWindow, error) {
	var found *limitWindow
	for _, candidate := range []*limitWindow{snapshot.Primary, snapshot.Secondary} {
		if candidate == nil || candidate.WindowDurationMins == nil || *candidate.WindowDurationMins != mins {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("%w: %d minutes", ErrWindowAmbiguous, mins)
		}
		found = candidate
	}
	if found == nil && mins == fiveHourWindowMins {
		return nil, ErrFiveHourWindowMissing
	}
	return found, nil
}

func toWindow(window *limitWindow, mins int64) Window {
	converted := Window{
		UsedPercent:        window.UsedPercent,
		WindowDurationMins: mins,
		ResetsAt:           window.ResetsAt,
	}
	if window.ResetsAt != nil {
		rfc3339 := time.Unix(*window.ResetsAt, 0).UTC().Format(time.RFC3339)
		converted.ResetsAtRFC3339 = &rfc3339
	}
	return converted
}

func withStderrTail(err error, stderr string) error {
	tail := stderrTail(stderr)
	if tail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, tail)
}

func stderrTail(stderr string) string {
	text := strings.TrimSpace(stderr)
	if len(text) > stderrTailLimit {
		text = text[len(text)-stderrTailLimit:]
	}
	return text
}

func truncateLine(line string) string {
	if len(line) > 200 {
		return line[:200]
	}
	return line
}
