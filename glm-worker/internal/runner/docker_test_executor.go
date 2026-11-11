package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/dockertest"
)

type dockerTestRunFunc func(ctx context.Context, repository string, args []string, stdout, stderr io.Writer) (int, error)

type dockerTestExecutor struct {
	guard          *gitAuthorityGuard
	run            dockerTestRunFunc
	scanInterval   time.Duration
	requestTimeout time.Duration
	maxRequestSize int
	maxOutputSize  int
	maxRequests    int

	baseCtx              context.Context
	cancel               context.CancelFunc
	stopOnce             sync.Once
	stopCh               chan struct{}
	doneCh               chan struct{}
	consumed             map[string]uint64
	processed            int
	stagingFailureLogged bool
	pins                 dockerTestBoundaryPins
}

type stagingDirent struct {
	name    string
	inode   uint64
	regular bool
}

type stagingRequestCandidate struct {
	name  string
	token string
	kind  string
	inode uint64
}

type stagingDirIdentity struct {
	device uint64
	inode  uint64
}

type dockerTestBoundaryPins struct {
	tempDir       stagingDirIdentity
	workerTempDir stagingDirIdentity
	stagingDir    stagingDirIdentity
}

type cappedBuffer struct {
	buf       strings.Builder
	limit     int
	truncated bool
}

const (
	dockerTestStagingDirName  = "docker-test"
	dockerTestResponseDirName = "docker-test-responses"

	dockerTestScanIntervalDefault = 250 * time.Millisecond
	dockerTestMaxRequestBytes     = 1 << 20
	dockerTestMaxOutputBytes      = 16 << 20
	dockerTestMaxRequests         = 64

	stagingCandidateRun    = "run"
	stagingCandidateReuse  = "token-reuse"
	stagingCandidateOther  = "special-file"
	stagingCandidateName   = "malformed-name"
	stagingCandidateBudget = "request-budget"
	stagingCandidateRace   = "concurrent-request"
)

var (
	errStagingResponseExists      = errors.New("staging responseが既に存在します")
	errStagingDirIdentityMismatch = errors.New("staging directoryのidentityが固定値と一致しません")
)

func (g *gitAuthorityGuard) startDockerTestExecutor() (*dockerTestExecutor, error) {
	if g == nil || !g.dockerPolicy || g.dockerTestStagingDir == "" {
		return nil, nil
	}
	run := g.dockerTestRun
	if run == nil {
		run = dockertest.Run
	}
	requestTimeout := g.dockerTestRequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = dockertest.StagingRequestTimeoutDefault
	}
	scanInterval := g.dockerTestScanInterval
	if scanInterval <= 0 {
		scanInterval = dockerTestScanIntervalDefault
	}
	maxRequestSize := g.dockerTestMaxRequestBytes
	if maxRequestSize <= 0 {
		maxRequestSize = dockerTestMaxRequestBytes
	}
	maxOutputSize := g.dockerTestMaxOutputBytes
	if maxOutputSize <= 0 {
		maxOutputSize = dockerTestMaxOutputBytes
	}
	maxRequests := g.dockerTestMaxRequests
	if maxRequests <= 0 {
		maxRequests = dockerTestMaxRequests
	}
	pins, err := g.pinDockerTestBoundary()
	if err != nil {
		return nil, err
	}
	baseCtx, cancel := context.WithCancel(context.Background())
	executor := &dockerTestExecutor{
		guard:          g,
		run:            run,
		scanInterval:   scanInterval,
		requestTimeout: requestTimeout,
		maxRequestSize: maxRequestSize,
		maxOutputSize:  maxOutputSize,
		maxRequests:    maxRequests,
		baseCtx:        baseCtx,
		cancel:         cancel,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
		consumed:       make(map[string]uint64),
		pins:           pins,
	}
	go executor.loop()
	return executor, nil
}

func (g *gitAuthorityGuard) pinDockerTestBoundary() (dockerTestBoundaryPins, error) {
	tempDir, err := pinStagingDirPath(g.tempDir)
	if err != nil {
		return dockerTestBoundaryPins{}, err
	}
	workerTempDir, err := pinStagingDirPath(g.workerTempDir)
	if err != nil {
		return dockerTestBoundaryPins{}, err
	}
	stagingDir, err := pinStagingDirPath(g.dockerTestStagingDir)
	if err != nil {
		return dockerTestBoundaryPins{}, err
	}
	return dockerTestBoundaryPins{tempDir: tempDir, workerTempDir: workerTempDir, stagingDir: stagingDir}, nil
}

func pinStagingDirPath(path string) (stagingDirIdentity, error) {
	identity, ok := pinStagingDirIdentity(path)
	if !ok {
		return stagingDirIdentity{}, fmt.Errorf("docker test境界のidentityを固定できません: %s", path)
	}
	return identity, nil
}

func (e *dockerTestExecutor) stop() {
	if e == nil {
		return
	}
	e.stopOnce.Do(func() {
		close(e.stopCh)
		e.cancel()
	})
	<-e.doneCh
}

func (e *dockerTestExecutor) loop() {
	defer close(e.doneCh)
	ticker := time.NewTicker(e.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-e.baseCtx.Done():
			return
		case <-ticker.C:
			e.scanOnce()
		}
	}
}

func (e *dockerTestExecutor) scanOnce() {
	if !e.stagingIntact() {
		e.recordStagingFailure("docker-test:staging-replaced")
		return
	}
	entries, err := readStagingDirents(e.guard.dockerTestStagingDir, e.pins.stagingDir)
	if err != nil {
		if errors.Is(err, errStagingDirIdentityMismatch) {
			e.recordStagingFailure("docker-test:staging-replaced")
			return
		}
		e.recordStagingFailure("docker-test:staging-scan:" + err.Error())
		return
	}
	e.handleStagingDirents(entries)
}

func readStagingDirents(dir string, pinned stagingDirIdentity) ([]stagingDirent, error) {
	file, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	identity, ok := stagingDirFileIdentity(file)
	if !ok || identity != pinned {
		return nil, errStagingDirIdentityMismatch
	}
	return readStagingDirentsFD(file)
}

func (e *dockerTestExecutor) handleStagingDirents(entries []stagingDirent) {
	pending, violations := e.classifyStagingDirents(entries)
	for _, candidate := range violations {
		e.consume(candidate)
		e.rejectCandidate(candidate)
	}
	if len(pending) == 0 {
		return
	}
	for _, candidate := range pending[1:] {
		candidate.kind = stagingCandidateRace
		e.consume(candidate)
		e.rejectCandidate(candidate)
	}
	first := pending[0]
	e.consume(first)
	e.processRequest(first)
}

func (e *dockerTestExecutor) classifyStagingDirents(entries []stagingDirent) ([]stagingRequestCandidate, []stagingRequestCandidate) {
	pending := make([]stagingRequestCandidate, 0, len(entries))
	violations := make([]stagingRequestCandidate, 0)
	for _, entry := range entries {
		if !dockertest.IsStagingRequestFileName(entry.name) {
			continue
		}
		token, tokenOK := dockertest.StagingTokenFromRequestFileName(entry.name)
		if !entry.regular {
			violations = append(violations, newStagingRequestCandidate(entry.name, token, stagingCandidateOther, entry.inode))
			continue
		}
		if !tokenOK {
			violations = append(violations, newStagingRequestCandidate(entry.name, token, stagingCandidateName, entry.inode))
			continue
		}
		candidate := newStagingRequestCandidate(entry.name, token, stagingCandidateRun, entry.inode)
		if consumed, seen := e.consumed[entry.name]; seen {
			if consumed == candidate.inode {
				continue
			}
			candidate.kind = stagingCandidateReuse
			violations = append(violations, candidate)
			continue
		}
		pending = append(pending, candidate)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].name < pending[j].name })
	return pending, violations
}

func newStagingRequestCandidate(name, token, kind string, inode uint64) stagingRequestCandidate {
	return stagingRequestCandidate{name: name, token: token, kind: kind, inode: inode}
}

func (e *dockerTestExecutor) consume(candidate stagingRequestCandidate) {
	e.consumed[candidate.name] = candidate.inode
}

func (e *dockerTestExecutor) rejectCandidate(candidate stagingRequestCandidate) {
	reasons := map[string]string{
		stagingCandidateRace:   "並行staging requestを拒否しました",
		stagingCandidateReuse:  "再利用されたstaging request tokenを拒否しました",
		stagingCandidateOther:  "request fileが通常fileではありません",
		stagingCandidateBudget: "staging request数がsession上限を超えました",
	}
	if reason, ok := reasons[candidate.kind]; ok && candidate.token != "" {
		e.rejectStagingRequest(candidate.token, reason)
	}
	e.recordAttempt("docker-test:" + candidate.kind + ":" + candidate.name)
}

func (e *dockerTestExecutor) processRequest(candidate stagingRequestCandidate) {
	if e.processed >= e.maxRequests {
		budget := candidate
		budget.kind = stagingCandidateBudget
		e.rejectCandidate(budget)
		return
	}
	request, err := e.readRequest(candidate)
	if err != nil {
		e.rejectStagingRequest(candidate.token, err.Error())
		e.recordAttempt("docker-test:rejected-request:" + candidate.token + ":" + err.Error())
		return
	}
	// A response may survive a timed-out client. Never execute a second
	// request with that identity: it could otherwise consume an old success.
	responsePath := filepath.Join(e.guard.dockerTestResponseDir, dockertest.StagingResponseFileName(request.Token))
	if _, err := os.Lstat(responsePath); err == nil {
		e.recordAttempt("docker-test:response-exists:" + request.Token)
		return
	} else if !os.IsNotExist(err) {
		e.recordAttempt("docker-test:response-stat:" + request.Token + ":" + err.Error())
		return
	}
	e.processed++
	e.executeRequest(request)
}

func (e *dockerTestExecutor) readRequest(candidate stagingRequestCandidate) (dockertest.StagingRequest, error) {
	file, err := openStagingRequestFile(e.guard.dockerTestStagingDir, candidate.name)
	if err != nil {
		return dockertest.StagingRequest{}, fmt.Errorf("request fileを開けません")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return dockertest.StagingRequest{}, fmt.Errorf("request fileが通常fileではありません")
	}
	if verifyErr := e.verifyStagingRequestFile(info, candidate); verifyErr != nil {
		return dockertest.StagingRequest{}, verifyErr
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(e.maxRequestSize)+1))
	if err != nil {
		return dockertest.StagingRequest{}, fmt.Errorf("request fileを読めません")
	}
	if len(data) > e.maxRequestSize {
		return dockertest.StagingRequest{}, fmt.Errorf("request fileがsize上限を超えています")
	}
	request, err := dockertest.ParseStagingRequest(data)
	if err != nil {
		return dockertest.StagingRequest{}, err
	}
	if request.Token != candidate.token {
		return dockertest.StagingRequest{}, fmt.Errorf("request file名とtokenが一致しません")
	}
	if !e.stagingIntact() {
		return dockertest.StagingRequest{}, fmt.Errorf("staging directoryが実行中に置換されました")
	}
	return request, nil
}

func (e *dockerTestExecutor) verifyStagingRequestFile(info os.FileInfo, candidate stagingRequestCandidate) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("request fileが通常fileではありません")
	}
	identity, ok := stagingFileIdentity(info)
	if !ok || identity.device != e.pins.stagingDir.device || identity.inode != candidate.inode {
		return fmt.Errorf("request fileが固定staging directoryの列挙内容と一致しません")
	}
	if info.Size() > int64(e.maxRequestSize) {
		return fmt.Errorf("request fileがsize上限を超えています")
	}
	return nil
}

func (e *dockerTestExecutor) executeRequest(request dockertest.StagingRequest) {
	ctx, cancel := context.WithTimeout(e.baseCtx, e.requestTimeout)
	defer cancel()
	stdout := newCappedBuffer(e.maxOutputSize)
	stderr := newCappedBuffer(e.maxOutputSize)
	code, runErr := e.run(ctx, e.guard.repoRoot, request.Args, stdout, stderr)
	response := dockertest.StagingResponse{
		SchemaVersion: dockertest.StagingSchemaVersion,
		Token:         request.Token,
		ExitCode:      code,
		Stdout:        stdout.String(),
		Stderr:        stderr.String(),
	}
	if runErr != nil {
		response.Failure = runErr.Error()
		if response.ExitCode == 0 {
			response.ExitCode = 1
		}
	}
	if stdout.truncated || stderr.truncated {
		response.Failure = strings.TrimSpace(response.Failure + " 出力がstaging response上限を超えました")
		if response.ExitCode == 0 {
			response.ExitCode = 1
		}
	}
	if err := writeStagingResponse(e.guard.dockerTestResponseDir, response); err != nil {
		e.recordResponseWriteFailure(request.Token, err)
	}
}

func (e *dockerTestExecutor) rejectStagingRequest(token, reason string) {
	response := dockertest.StagingResponse{
		SchemaVersion: dockertest.StagingSchemaVersion,
		Token:         token,
		ExitCode:      1,
		Failure:       reason,
	}
	if err := writeStagingResponse(e.guard.dockerTestResponseDir, response); err != nil {
		e.recordResponseWriteFailure(token, err)
	}
}

func (e *dockerTestExecutor) recordResponseWriteFailure(token string, err error) {
	if errors.Is(err, errStagingResponseExists) {
		e.recordAttempt("docker-test:response-exists:" + token)
		return
	}
	e.recordAttempt("docker-test:response-write:" + token + ":" + err.Error())
}

func (e *dockerTestExecutor) stagingIntact() bool {
	pins := []struct {
		path   string
		pinned stagingDirIdentity
	}{
		{e.guard.tempDir, e.pins.tempDir},
		{e.guard.workerTempDir, e.pins.workerTempDir},
		{e.guard.dockerTestStagingDir, e.pins.stagingDir},
	}
	for _, pin := range pins {
		current, ok := pinStagingDirIdentity(pin.path)
		if !ok || current != pin.pinned {
			return false
		}
	}
	return true
}

func (e *dockerTestExecutor) recordStagingFailure(reason string) {
	if e.stagingFailureLogged {
		return
	}
	e.stagingFailureLogged = true
	e.recordAttempt(reason)
}

func (e *dockerTestExecutor) recordAttempt(reason string) {
	appendGitAuthorityAttempt(e.guard.attemptLog, reason)
}

func writeStagingResponse(dir string, response dockertest.StagingResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	file, err := createStagingResponseFile(filepath.Join(dir, dockertest.StagingResponseFileName(response.Token)))
	if err != nil {
		if os.IsExist(err) {
			return errStagingResponseExists
		}
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = file.Write(data)
	return err
}

func appendGitAuthorityAttempt(path, reason string) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	reason = strings.NewReplacer("\n", " ", "\r", " ").Replace(reason)
	_, _ = file.WriteString(reason + "\n")
}

func dockerCLIDenyScript(attemptLog string) string {
	return "#!/bin/sh\n" +
		"printf '%s\\n' '{\"error\":{\"kind\":\"containment_denial\",\"owner\":\"docker-test-owner\",\"reason\":\"docker_cli_blocked\",\"next_action\":\"use_goquality_test_entry_owner_executes_docker\"}}' >&2\n" +
		"{ printf 'docker %s\\n' \"$*\" >>" + shellSingleQuote(attemptLog) + "; } 2>/dev/null || :\n" +
		"exit 97\n"
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (b *cappedBuffer) Write(data []byte) (int, error) {
	if b.truncated {
		return len(data), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 || len(data) > remaining {
		b.truncated = true
		if remaining > 0 {
			b.buf.Write(data[:remaining])
		}
		return len(data), nil
	}
	b.buf.Write(data)
	return len(data), nil
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}
