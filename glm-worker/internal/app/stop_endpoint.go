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
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// stopEndpointFileは--stop要求を受け付ける単一目的local control endpointのsocket識別子。
// repo lock保有中のinvocationだけがlistenする。
const stopEndpointFile = "stop.sock"

// stopRequestLineはendpointが受理する唯一の要求行。任意のmessage injectionを受け付けず、
// この固定形以外は停止を行わない。
const stopRequestLine = "{\"type\":\"stop\",\"version\":1}\n"

const (
	// stopDialTimeoutはendpointへの接続待ち。local unix socketのため短い。
	stopDialTimeout = 5 * time.Second
	// stopHandshakeTimeoutは要求送出からowner側ack受取までの上限。owner側のTERM猶予・
	// KILL後続確認・interrupted状態保存をカバーする。
	stopHandshakeTimeout = 90 * time.Second
	// stopRequestReadTimeoutはowner側が要求行を読むまでの上限。
	stopRequestReadTimeout = 10 * time.Second
	stopResponseLimit      = 64 * 1024
	// stopEndpointMaxPathはunix socketのsun_path長上限(macOS 104byte・Linux 108byte)に
	// 余裕を持たせたendpoint pathの長さ上限。
	stopEndpointMaxPath = 100
)

// StopEndpointErrorは--stopの対象endpointが停止authorityとして応答できない失敗。
// Absentはendpoint file自体がなくrunning invocationが不在、staleはfileが残っていても
// 接続・ackが得られないことを表す。
type StopEndpointError struct {
	Absent bool
}

func (e *StopEndpointError) Error() string {
	if e.Absent {
		return "no running glm-worker holds the stop endpoint for this repository"
	}
	return "stop endpoint did not acknowledge the stop request"
}

// stopOutputは--stop成功時のmachine contract。owner側ackの結果をそのまま載せる。
type stopOutput struct {
	Result          string  `json:"result"`
	TaskID          *string `json:"task_id"`
	TaskStatus      *string `json:"task_status"`
	ResumeAvailable bool    `json:"resume_available"`
}

// stopEndpointResponseはowner側ackの1行JSON。
type stopEndpointResponse struct {
	Result          string  `json:"result"`
	TaskID          *string `json:"task_id"`
	TaskStatus      *string `json:"task_status"`
	ResumeAvailable bool    `json:"resume_available"`
}

// stopEndpointPathは--stop endpointのunix socket file path。第一候補はstate dir配下の
// stop.sockだが、そのpathはunix socketのsun_path長上限(macOS 104byte等)を超え得るため、
// 長すぎるときはstate dir pathで一意に決まるhash名を固定短縮base配下へ置く。候補選択は
// path長だけから決まるため、同一cfgのowner/requesterが同じpathへ到達する。
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

// requestStopは固定stop要求をlocal control endpointへ送り、ackをmachine JSONで出す。
// endpoint不在・staleはtyped errorへ、state書込・repo lock取得は行わない。
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
	defer conn.Close()
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
	return writeJSON(stdout, stopOutput{
		Result:          response.Result,
		TaskID:          response.TaskID,
		TaskStatus:      response.TaskStatus,
		ResumeAvailable: response.ResumeAvailable,
	})
}

// readStopEndpointResponseはack 1行を読んで停止結果の受理集合へ検証する。ownerはack
// 書込み後に接続を閉じるため、上限付きの読み取りで全体を受け取る。
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
	case "interrupted", "terminal", "exited":
		return response, nil
	default:
		return stopEndpointResponse{}, fmt.Errorf("stop endpoint returned unknown result: %q", response.Result)
	}
}

// stopEndpointServerはrunning invocationがlock保有中に開く単一目的control endpoint。
// 固定stop要求だけを受理し、停止の確定結果で1行ackする。
type stopEndpointServer struct {
	listener   net.Listener
	controller *runner.StopController
	st         *state.StateStore
	path       string
	done       chan struct{}
}

// startStopEndpointはendpointを開いてaccept loopを始める。repo lock保有者は対象
// repositoryで唯一のownerであるため、残存socket fileは前回invocationのstale endpoint
// として除いてからlistenする。socketは同一userのowner/requester間だけで使うため
// 接続権をownerへ限定する。
func startStopEndpoint(st *state.StateStore, controller *runner.StopController) (*stopEndpointServer, error) {
	path := stopEndpointPath(st)
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("GLM worker停止endpointを開けません: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
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
		go s.handleConn(conn)
	}
}

func (s *stopEndpointServer) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(stopRequestReadTimeout))
	reader := bufio.NewReader(io.LimitReader(conn, stopResponseLimit+1))
	line, err := reader.ReadString('\n')
	if err != nil || len(line) > stopResponseLimit || strings.TrimRight(line, "\r\n") != strings.TrimRight(stopRequestLine, "\n") {
		s.writeResponse(conn, stopEndpointResponse{Result: "rejected"})
		return
	}
	// 要求読み取り後は停止確定(TERM猶予・KILL後続確認・状態保存)まで待つため、ack書込みの
	// deadlineをhandshake上限へ延長する。
	_ = conn.SetDeadline(time.Now().Add(stopHandshakeTimeout))
	s.controller.Request()
	outcome := s.controller.WaitOutcome()
	s.writeResponse(conn, s.buildResponse(outcome))
}

// buildResponseは停止確定結果をackへ組み立てる。中断時はinterrupted checkpoint保存済みの
// task IDとstatusで応答し、自然終端時は現在のauthoritative statusで応答する。
func (s *stopEndpointServer) buildResponse(outcome runner.StopOutcome) stopEndpointResponse {
	if outcome.Interrupted {
		status := "interrupted"
		return stopEndpointResponse{
			Result:          "interrupted",
			TaskID:          stringPtr(outcome.TaskID),
			TaskStatus:      &status,
			ResumeAvailable: true,
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

// stopFinishedResultは停止より先に invocationが終端したときの結果分類。terminal statusへ
// 到達済みならterminal、それ以外(activeなど)はexitedで区別する。
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

// checkpointResumeAvailableは保存済みcheckpointが--resumeで再開可能な停止状態か。
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

// Closeはendpointを閉じる。停止が確定しないまま残った要求へは現在状態で終端ackを返し、
// socket fileを除去する。
func (s *stopEndpointServer) Close() {
	s.controller.NotifyFinished()
	_ = s.listener.Close()
	<-s.done
	_ = os.Remove(s.path)
}
