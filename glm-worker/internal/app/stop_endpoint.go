package app

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type StopEndpointError struct {
	Absent bool
}

type stopOutput struct {
	Result          string  `json:"result"`
	TaskID          *string `json:"task_id"`
	TaskStatus      *string `json:"task_status"`
	ResumeAvailable bool    `json:"resume_available"`
	CleanupWarning  string  `json:"cleanup_warning,omitempty"`
}

type stopEndpointResponse struct {
	Result          string  `json:"result"`
	TaskID          *string `json:"task_id"`
	TaskStatus      *string `json:"task_status"`
	ResumeAvailable bool    `json:"resume_available"`
	CleanupWarning  string  `json:"cleanup_warning,omitempty"`
}

type stopEndpointServer struct {
	listener   net.Listener
	controller *runner.StopController
	st         *state.StateStore
	path       string
	done       chan struct{}

	handlers sync.WaitGroup
}

const stopEndpointFile = "stop.sock"

const stopRequestLine = "{\"type\":\"stop\",\"version\":1}\n"

const (
	stopDialTimeout = 5 * time.Second

	stopHandshakeTimeout = 90 * time.Second

	stopRequestReadTimeout = 10 * time.Second
	stopResponseLimit      = 64 * 1024

	stopEndpointMaxPath = 100
)

const (
	stopResultInterrupted         = "interrupted"
	stopResultInterruptedResidual = "interrupted_cleanup_residual"
)

func (e *StopEndpointError) Error() string {
	if e.Absent {
		return "no running glm-worker holds the stop endpoint for this repository"
	}
	return "stop endpoint did not acknowledge the stop request"
}

func stopEndpointPath(st *state.StateStore) string {
	sum := sha256.Sum256([]byte(st.Path(stopEndpointFile)))
	fallback := filepath.Join("/tmp", fmt.Sprintf("glm-worker-stop-%s.sock", hex.EncodeToString(sum[:8])))
	for _, candidate := range []string{st.Path(stopEndpointFile), fallback} {
		if len(candidate) <= stopEndpointMaxPath {
			return candidate
		}
	}
	return fallback
}

func requestStop(cfg config.AppConfig, stdout io.Writer) error {
	path := stopEndpointPath(state.AttachStateStore(cfg))
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &StopEndpointError{Absent: true}
		}
		return fmt.Errorf("停止endpointの状態を取得できません: %w", err)
	}
	conn, err := net.DialTimeout("unix", path, stopDialTimeout)
	if err != nil {
		return &StopEndpointError{}
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(stopHandshakeTimeout)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte(stopRequestLine)); err != nil {
		return &StopEndpointError{}
	}
	response, err := readStopEndpointResponse(conn)
	if err != nil {
		return &StopEndpointError{}
	}
	return writeJSON(stdout, stopOutput(response))
}

func readStopEndpointResponse(conn net.Conn) (stopEndpointResponse, error) {
	raw, err := io.ReadAll(io.LimitReader(conn, stopResponseLimit+1))
	if err != nil {
		return stopEndpointResponse{}, err
	}
	if len(raw) > stopResponseLimit {
		return stopEndpointResponse{}, fmt.Errorf("stop endpoint response exceeds the size limit")
	}
	var response stopEndpointResponse
	if err := json.Unmarshal(bytes.TrimSpace(raw), &response); err != nil {
		return stopEndpointResponse{}, err
	}
	switch response.Result {
	case stopResultInterrupted, stopResultInterruptedResidual, "terminal", "exited":
		return response, nil
	default:
		return stopEndpointResponse{}, fmt.Errorf("stop endpoint returned unknown result: %q", response.Result)
	}
}

func startStopEndpoint(st *state.StateStore, controller *runner.StopController) (*stopEndpointServer, error) {
	path := stopEndpointPath(st)
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("GLM worker停止endpointを開けません: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("GLM worker停止endpointの接続権を限定できません: %w", err)
	}
	server := &stopEndpointServer{
		listener:   listener,
		controller: controller,
		st:         st,
		path:       path,
		done:       make(chan struct{}),
	}
	go server.acceptLoop()
	return server, nil
}

func (s *stopEndpointServer) acceptLoop() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		s.handlers.Add(1)
		go s.handleConn(conn)
	}
}

func (s *stopEndpointServer) handleConn(conn net.Conn) {
	defer s.handlers.Done()
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(stopRequestReadTimeout))
	reader := bufio.NewReader(io.LimitReader(conn, stopResponseLimit+1))
	line, err := reader.ReadString('\n')
	if err != nil || len(line) > stopResponseLimit || strings.TrimRight(line, "\r\n") != strings.TrimRight(stopRequestLine, "\n") {
		s.writeResponse(conn, stopEndpointResponse{Result: "rejected"})
		return
	}

	_ = conn.SetDeadline(time.Now().Add(stopHandshakeTimeout))
	s.controller.Request()
	outcome := s.controller.WaitOutcome()
	s.writeResponse(conn, s.buildResponse(outcome))
}

func (s *stopEndpointServer) buildResponse(outcome runner.StopOutcome) stopEndpointResponse {
	if outcome.Interrupted {
		status := "interrupted"
		result := stopResultInterrupted
		if outcome.CleanupWarning != "" {
			result = stopResultInterruptedResidual
		}
		return stopEndpointResponse{
			Result:          result,
			TaskID:          stringPtr(outcome.TaskID),
			TaskStatus:      &status,
			ResumeAvailable: true,
			CleanupWarning:  outcome.CleanupWarning,
		}
	}
	response := stopEndpointResponse{Result: stopFinishedResult(s.st.TaskStatus())}
	if id := s.st.ReadOr("task.id", ""); id != "" {
		response.TaskID = stringPtr(id)
	}
	response.TaskStatus = taskStatusPtr(s.st.TaskStatus())
	response.ResumeAvailable = checkpointResumeAvailable(s.st)
	return response
}

func stopFinishedResult(status state.TaskStatus) string {
	switch status {
	case state.TaskStatusComplete,
		state.TaskStatusWaitingDecision,
		state.TaskStatusWaitingSolReview,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusInterrupted:
		return "terminal"
	default:
		return "exited"
	}
}

func checkpointResumeAvailable(st *state.StateStore) bool {
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return false
	}
	return checkpoint.RateLimited || checkpoint.ProviderUnavailable || checkpoint.UserInterrupted
}

func (s *stopEndpointServer) writeResponse(conn net.Conn, response stopEndpointResponse) {
	data, err := marshalEventLine(response)
	if err != nil {
		return
	}
	_, _ = conn.Write(data)
}

func (s *stopEndpointServer) Close() {
	s.controller.NotifyFinished()
	_ = s.listener.Close()
	<-s.done
	s.handlers.Wait()
	_ = os.Remove(s.path)
}
