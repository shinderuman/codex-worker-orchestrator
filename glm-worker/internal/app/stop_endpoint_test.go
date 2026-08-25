package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func stopEndpointTestStore(t *testing.T) (*state.StateStore, config.AppConfig) {
	t.Helper()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return st, cfg
}

func TestRequestStopFailsWhenEndpointAbsent(t *testing.T) {
	_, cfg := stopEndpointTestStore(t)
	err := requestStop(cfg, &strings.Builder{})
	var endpointErr *StopEndpointError
	if !errors.As(err, &endpointErr) || !endpointErr.Absent {
		t.Fatalf("absent endpoint errorを期待: %v", err)
	}
	if buildProcessError(err).Kind != errorKindStopEndpointAbsent {
		t.Fatalf("process error kind = %s want stop_endpoint_absent", buildProcessError(err).Kind)
	}
}

func TestRequestStopFailsWhenEndpointIsStale(t *testing.T) {
	st, cfg := stopEndpointTestStore(t)
	path := stopEndpointPath(st)
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	err := requestStop(cfg, &strings.Builder{})
	var endpointErr *StopEndpointError
	if !errors.As(err, &endpointErr) || endpointErr.Absent {
		t.Fatalf("stale endpoint errorを期待: %v", err)
	}
	if buildProcessError(err).Kind != errorKindStopEndpointStale {
		t.Fatalf("process error kind = %s want stop_endpoint_stale", buildProcessError(err).Kind)
	}
}

func startStopEndpointForTest(t *testing.T, st *state.StateStore) (*stopEndpointServer, *runner.StopController, net.Conn) {
	t.Helper()
	controller := runner.NewStopController()
	server, err := startStopEndpoint(st, controller)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	conn, err := net.DialTimeout("unix", stopEndpointPath(st), stopDialTimeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return server, controller, conn
}

func TestStopEndpointRejectsUnknownRequestLine(t *testing.T) {
	st, _ := stopEndpointTestStore(t)
	_, _, conn := startStopEndpointForTest(t, st)
	if _, err := conn.Write([]byte("{\"type\":\"inject\",\"message\":\"x\"}\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(stopRequestReadTimeout))
	response := readStopEndpointResponseForTest(t, conn)
	if response.Result != "rejected" {
		t.Fatalf("未知要求行の応答 = %#v want rejected", response)
	}
}

func readStopEndpointResponseForTest(t *testing.T, conn net.Conn) stopEndpointResponse {
	t.Helper()
	raw := make([]byte, stopResponseLimit)
	n, err := conn.Read(raw)
	if err != nil && n == 0 {
		t.Fatal(err)
	}
	var response stopEndpointResponse
	if err := json.Unmarshal(bytes.TrimSpace(raw[:n]), &response); err != nil {
		t.Fatalf("ack行をJSON化できません: %s", raw[:n])
	}
	return response
}

func TestStopEndpointAcknowledgesInterruptedStop(t *testing.T) {
	st, cfg := stopEndpointTestStore(t)
	_, controller, _ := startStopEndpointForTest(t, st)

	var out strings.Builder
	done := make(chan error, 1)
	go func() { done <- requestStop(cfg, &out) }()
	controller.NotifyInterrupted("task-interrupted-1", "")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop要求がackを返しません")
	}
	if !strings.Contains(out.String(), `"result":"interrupted"`) ||
		!strings.Contains(out.String(), `"task_id":"task-interrupted-1"`) ||
		!strings.Contains(out.String(), `"task_status":"interrupted"`) ||
		!strings.Contains(out.String(), `"resume_available":true`) {
		t.Fatalf("interrupted ack = %s", out.String())
	}
}

func TestStopEndpointAcknowledgesConcurrentStopRequests(t *testing.T) {
	st, cfg := stopEndpointTestStore(t)
	_, controller, _ := startStopEndpointForTest(t, st)

	outs := make([]strings.Builder, 2)
	done := make(chan error, 2)
	for i := range outs {
		go func(i int) { done <- requestStop(cfg, &outs[i]) }(i)
	}

	time.Sleep(50 * time.Millisecond)
	controller.NotifyInterrupted("task-concurrent-stop", "")

	for i := range outs {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("同時stop要求 %d がackを返しません", i)
		}
	}
	for i := range outs {
		if !strings.Contains(outs[i].String(), `"result":"interrupted"`) ||
			!strings.Contains(outs[i].String(), `"task_id":"task-concurrent-stop"`) ||
			!strings.Contains(outs[i].String(), `"resume_available":true`) {
			t.Fatalf("同時stop要求 %d のack = %s", i, outs[i].String())
		}
	}
}

func TestStopEndpointAcknowledgesCleanupResidualAsTypedOutcome(t *testing.T) {
	st, cfg := stopEndpointTestStore(t)
	_, controller, _ := startStopEndpointForTest(t, st)

	var out strings.Builder
	done := make(chan error, 1)
	go func() { done <- requestStop(cfg, &out) }()
	controller.NotifyInterrupted("task-residual-1", "process group 424242に残存processがあります")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop要求がackを返しません")
	}
	if !strings.Contains(out.String(), `"result":"interrupted_cleanup_residual"`) ||
		!strings.Contains(out.String(), `"task_id":"task-residual-1"`) ||
		!strings.Contains(out.String(), `"task_status":"interrupted"`) ||
		!strings.Contains(out.String(), `"resume_available":true`) ||
		!strings.Contains(out.String(), `"cleanup_warning":"process group 424242に残存processがあります"`) {
		t.Fatalf("残存時ack = %s", out.String())
	}
	if strings.Contains(out.String(), `"result":"interrupted"`) {
		t.Fatalf("残存時ackが安全停止resultを返しています: %s", out.String())
	}
}

func TestStopEndpointCloseWaitsForPendingRequestAck(t *testing.T) {
	st, _ := stopEndpointTestStore(t)
	controller := runner.NewStopController()
	server, err := startStopEndpoint(st, controller)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("unix", stopEndpointPath(st), stopDialTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(stopRequestLine)); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	server.Close()
	if _, err := os.Stat(stopEndpointPath(st)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Close後にsocket fileが残っています: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	response := readStopEndpointResponseForTest(t, conn)
	if response.Result != "terminal" && response.Result != "exited" {
		t.Fatalf("Close後のpending要求ack = %#v want terminal/exited", response)
	}
	if response.TaskID == nil || *response.TaskID == "" {
		t.Fatalf("Close後のpending要求ackがtask IDを失っています: %#v", response)
	}
}

func TestStopEndpointAcknowledgesNaturalTerminalAsTerminal(t *testing.T) {
	st, cfg := stopEndpointTestStore(t)
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	_, controller, _ := startStopEndpointForTest(t, st)

	var out strings.Builder
	done := make(chan error, 1)
	go func() { done <- requestStop(cfg, &out) }()
	controller.NotifyFinished()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop要求がackを返しません")
	}
	if !strings.Contains(out.String(), `"result":"terminal"`) ||
		!strings.Contains(out.String(), `"task_status":"complete"`) ||
		strings.Contains(out.String(), `"resume_available":true`) {
		t.Fatalf("自然終端ack = %s", out.String())
	}
}

func TestStopEndpointReportsActiveExitAsExited(t *testing.T) {
	st, cfg := stopEndpointTestStore(t)
	_, controller, _ := startStopEndpointForTest(t, st)

	var out strings.Builder
	done := make(chan error, 1)
	go func() { done <- requestStop(cfg, &out) }()
	controller.NotifyFinished()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop要求がackを返しません")
	}
	if !strings.Contains(out.String(), `"result":"exited"`) {
		t.Fatalf("active終了ack = %s", out.String())
	}
}

func TestStopEndpointCloseRemovesSocketFile(t *testing.T) {
	st, _ := stopEndpointTestStore(t)
	controller := runner.NewStopController()
	server, err := startStopEndpoint(st, controller)
	if err != nil {
		t.Fatal(err)
	}
	path := stopEndpointPath(st)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket fileが残っています: %v", err)
	}
}

func TestStopEndpointPathFitsUnixSocketLimit(t *testing.T) {
	deepBase := t.TempDir()
	deep := deepBase
	for i := 0; i < 8; i++ {
		deep = filepath.Join(deep, "very-long-state-segment-name")
	}
	st := state.AttachStateStore(config.AppConfig{StateBase: deep, RepoHash: "apphash"})
	if got := stopEndpointPath(st); len(got) > stopEndpointMaxPath {
		t.Fatalf("endpoint path長 = %d want <= %d: %s", len(got), stopEndpointMaxPath, got)
	}
}
