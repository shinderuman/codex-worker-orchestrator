package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/dockertest"
)

type stagingRunRecord struct {
	root string
	args []string
}

type stagingRunRecorder struct {
	mu      sync.Mutex
	records []stagingRunRecord
}

func (r *stagingRunRecorder) capture(root string, args []string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, stagingRunRecord{root: root, args: append([]string(nil), args...)})
	return len(r.records)
}

func (r *stagingRunRecorder) snapshot() []stagingRunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]stagingRunRecord(nil), r.records...)
}

func newDockerTestGuard(t *testing.T) *gitAuthorityGuard {
	t.Helper()
	guard := &gitAuthorityGuard{
		repoRoot:     filepath.Join(t.TempDir(), "lane-repo"),
		realGit:      "git",
		before:       gitAuthoritySnapshot{active: true},
		dockerPolicy: true,
	}
	if err := os.MkdirAll(guard.repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := guard.prepareProxy(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(guard.cleanup)
	return guard
}

func startTestDockerTestExecutor(t *testing.T, guard *gitAuthorityGuard, run dockerTestRunFunc) *dockerTestExecutor {
	t.Helper()
	guard.dockerTestRun = run
	guard.dockerTestScanInterval = 5 * time.Millisecond
	executor, err := guard.startDockerTestExecutor()
	if err != nil {
		t.Fatal(err)
	}
	if executor == nil {
		t.Fatal("docker test executorが起動しませんでした")
	}
	t.Cleanup(executor.stop)
	return executor
}

func writeStagingRequestFile(t *testing.T, stagingDir, name, body string) {
	t.Helper()
	temp := filepath.Join(stagingDir, ".write-"+name+".tmp")
	if err := os.WriteFile(temp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temp, filepath.Join(stagingDir, name)); err != nil {
		t.Fatal(err)
	}
}

func stagingRequestBody(t *testing.T, token string, args ...string) string {
	t.Helper()
	data, err := json.Marshal(dockertest.StagingRequest{SchemaVersion: dockertest.StagingSchemaVersion, Token: token, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func waitStagingResponse(t *testing.T, responseDir, token string) dockertest.StagingResponse {
	t.Helper()
	return waitStagingResponseCondition(t, responseDir, token, func(dockertest.StagingResponse) bool { return true })
}

func waitStagingResponseCondition(t *testing.T, responseDir, token string, accept func(dockertest.StagingResponse) bool) dockertest.StagingResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(responseDir, dockertest.StagingResponseFileName(token)))
		if err == nil {
			response, parseErr := dockertest.ParseStagingResponse(data)
			if parseErr == nil && response.Token == token && accept(response) {
				return response
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("token %qへのstaging responseが条件を満たしませんでした", token)
	return dockertest.StagingResponse{}
}

func waitStagingCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("staging条件が時間内に成立しませんでした")
}

func dockerTestAttemptLogText(t *testing.T, guard *gitAuthorityGuard) string {
	t.Helper()
	data, err := os.ReadFile(guard.attemptLog)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDockerTestExecutorInactiveWithoutDockerPolicy(t *testing.T) {
	t.Parallel()
	guard := &gitAuthorityGuard{before: gitAuthoritySnapshot{active: true}}
	executor, err := guard.startDockerTestExecutor()
	if err != nil {
		t.Fatal(err)
	}
	if executor != nil {
		executor.stop()
		t.Fatal("Docker policyなしでexecutorが起動しました")
	}
}

func TestDockerPolicySessionKeepsSocketsAndDockerCLIOutOfWorker(t *testing.T) {
	guard := newDockerTestGuard(t)
	if filepath.Dir(guard.dockerTestStagingDir) != guard.workerTempDir {
		t.Fatalf("staging dir = %q がworker temporariesの外です: %q", guard.dockerTestStagingDir, guard.workerTempDir)
	}
	if strings.HasPrefix(guard.dockerTestResponseDir, guard.workerTempDir) {
		t.Fatalf("response dir = %q がworker書込可能領域の内側です", guard.dockerTestResponseDir)
	}
	wrapperScript := gitAuthorityClaudeWrapperScript(guard, "/bin/true")
	if !strings.Contains(wrapperScript, "unset DOCKER_HOST") || strings.Contains(wrapperScript, "unix://") {
		t.Fatalf("wrapper scriptがDocker endpoint到達を許しています: %s", wrapperScript)
	}
	if !strings.Contains(wrapperScript, "GLM_WORKER_DOCKER_TEST_STAGING="+shellSingleQuote(guard.dockerTestStagingDir)) {
		t.Fatalf("wrapper scriptがstaging dirをexportしません: %s", wrapperScript)
	}
	if !strings.Contains(wrapperScript, "GLM_WORKER_DOCKER_TEST_RESPONSES="+shellSingleQuote(guard.dockerTestResponseDir)) {
		t.Fatalf("wrapper scriptがresponse dirをexportしません: %s", wrapperScript)
	}

	dockerShim := filepath.Join(guard.tempDir, "docker")
	output, err := exec.Command(dockerShim, "version").CombinedOutput()
	if err == nil {
		t.Fatalf("docker CLI shimがdocker versionを許可しました: %s", output)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 97 {
		t.Fatalf("docker CLI shim終了 = %v output %q", err, output)
	}
	if !strings.Contains(string(output), "docker_cli_blocked") {
		t.Fatalf("docker CLI shimの拒否出力 = %q", output)
	}
	if log := dockerTestAttemptLogText(t, guard); !strings.Contains(log, "docker version") {
		t.Fatalf("docker CLI試行が機械記録に残っていません: %q", log)
	}
}

func TestDockerTestExecutorRejectsMalformedRequests(t *testing.T) {
	guard := newDockerTestGuard(t)
	recorder := &stagingRunRecorder{}
	run := func(_ context.Context, root string, args []string, _ io.Writer, _ io.Writer) (int, error) {
		recorder.capture(root, args)
		return 0, nil
	}
	startTestDockerTestExecutor(t, guard, run)

	cases := []struct {
		name  string
		token string
		body  string
	}{
		{name: "unknown-field", token: "2000000000000000000", body: `{"schema_version":1,"token":"2000000000000000000","args":["./..."],"mount":"/"}`},
		{name: "schema-mismatch", token: "2000000000000000001", body: `{"schema_version":2,"token":"2000000000000000001","args":["./..."]}`},
		{name: "missing-token", token: "2000000000000000002", body: `{"schema_version":1,"args":["./..."]}`},
		{name: "invalid-token", token: "2000000000000000003", body: `{"schema_version":1,"token":"../escape","args":["./..."]}`},
		{name: "empty-args", token: "2000000000000000004", body: `{"schema_version":1,"token":"2000000000000000004","args":[]}`},
		{name: "empty-arg-element", token: "2000000000000000005", body: `{"schema_version":1,"token":"2000000000000000005","args":[""]}`},
		{name: "terminator-arg", token: "2000000000000000006", body: `{"schema_version":1,"token":"2000000000000000006","args":["--","./..."]}`},
		{name: "args-not-array", token: "2000000000000000007", body: `{"schema_version":1,"token":"2000000000000000007","args":"./..."}`},
		{name: "token-mismatch", token: "2000000000000000008", body: `{"schema_version":1,"token":"9999999999999999999","args":["./..."]}`},
		{name: "trailing-json", token: "2000000000000000009", body: `{"schema_version":1,"token":"2000000000000000009","args":["./..."]}{"schema_version":1}`},
		{name: "non-object", token: "2000000000000000011", body: `["./..."]`},
	}
	for _, tc := range cases {
		writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(tc.token), tc.body)
		response := waitStagingResponse(t, guard.dockerTestResponseDir, tc.token)
		if response.ExitCode != 1 || response.Failure == "" {
			t.Fatalf("%s: 拒否 = code %d failure %q", tc.name, response.ExitCode, response.Failure)
		}
	}
	oversized := "2000000000000000010"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(oversized),
		`{"schema_version":1,"token":"`+oversized+`","args":["`+strings.Repeat("a", dockerTestMaxRequestBytes)+`"]}`)
	response := waitStagingResponse(t, guard.dockerTestResponseDir, oversized)
	if response.ExitCode != 1 || !strings.Contains(response.Failure, "size上限") {
		t.Fatalf("過大requestの拒否 = code %d failure %q", response.ExitCode, response.Failure)
	}

	if records := recorder.snapshot(); len(records) != 0 {
		t.Fatalf("不正requestがowner実行へ到達しました: %#v", records)
	}
	log := dockerTestAttemptLogText(t, guard)
	if !strings.Contains(log, "docker-test:rejected-request:") {
		t.Fatalf("protocol違反が機械記録に残っていません: %q", log)
	}
	if entries, _ := os.ReadDir(guard.dockerTestStagingDir); len(entries) != len(cases)+1 {
		t.Fatalf("ownerがstaging directoryへ書き込みました: %d entries", len(entries))
	}
}

func TestDockerTestExecutorRejectsConcurrentAndReusedTokens(t *testing.T) {
	guard := newDockerTestGuard(t)
	recorder := &stagingRunRecorder{}
	run := func(_ context.Context, root string, args []string, _ io.Writer, _ io.Writer) (int, error) {
		recorder.capture(root, args)
		return 0, nil
	}
	first := "3000000000000000000"
	second := "3000000000000000001"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(first), stagingRequestBody(t, first, "./internal/runner"))
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(second), stagingRequestBody(t, second, "./internal/runner"))
	startTestDockerTestExecutor(t, guard, run)

	firstResponse := waitStagingResponse(t, guard.dockerTestResponseDir, first)
	secondResponse := waitStagingResponse(t, guard.dockerTestResponseDir, second)
	if firstResponse.ExitCode != 0 {
		t.Fatalf("先着requestが拒否されました: %#v", firstResponse)
	}
	if secondResponse.ExitCode != 1 || !strings.Contains(secondResponse.Failure, "並行") {
		t.Fatalf("並行requestが拒否されていません: %#v", secondResponse)
	}
	if records := recorder.snapshot(); len(records) != 1 {
		t.Fatalf("並行requestで実行が単一になりません: %#v", records)
	}

	waitStagingCondition(t, func() bool { return len(recorder.snapshot()) == 1 })
	original := waitStagingResponse(t, guard.dockerTestResponseDir, first)
	if original.ExitCode != 0 {
		t.Fatalf("先着response = %#v", original)
	}
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(first), stagingRequestBody(t, first, "./internal/runner"))
	waitStagingCondition(t, func() bool {
		return strings.Contains(dockerTestAttemptLogText(t, guard), "docker-test:token-reuse:"+dockertest.StagingRequestFileName(first))
	})
	preserved := waitStagingResponse(t, guard.dockerTestResponseDir, first)
	if preserved.ExitCode != original.ExitCode || preserved.Failure != original.Failure || preserved.Stdout != original.Stdout || preserved.Stderr != original.Stderr {
		t.Fatalf("再利用拒否が既存responseを上書きしました: %#v", preserved)
	}
	if records := recorder.snapshot(); len(records) != 1 {
		t.Fatalf("再利用tokenがowner実行へ到達しました: %#v", records)
	}
	log := dockerTestAttemptLogText(t, guard)
	if !strings.Contains(log, "docker-test:concurrent-request:"+dockertest.StagingRequestFileName(second)) {
		t.Fatalf("並行拒否が機械記録に残っていません: %q", log)
	}
	if !strings.Contains(log, "docker-test:token-reuse:"+dockertest.StagingRequestFileName(first)) {
		t.Fatalf("再利用拒否が機械記録に残っていません: %q", log)
	}
}

func TestDockerTestExecutorFailsClosedOnRequestDeadline(t *testing.T) {
	guard := newDockerTestGuard(t)
	guard.dockerTestRequestTimeout = 60 * time.Millisecond
	run := func(ctx context.Context, _ string, _ []string, stdout, _ io.Writer) (int, error) {
		_, _ = io.WriteString(stdout, "partial-target-output\n")
		<-ctx.Done()
		return 1, ctx.Err()
	}
	startTestDockerTestExecutor(t, guard, run)
	token := "4000000000000000000"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(token), stagingRequestBody(t, token, "./..."))
	response := waitStagingResponse(t, guard.dockerTestResponseDir, token)
	if response.ExitCode == 0 || response.Failure == "" {
		t.Fatalf("締切失敗が報告されていません: %#v", response)
	}
	if !strings.Contains(response.Stdout, "partial-target-output") {
		t.Fatalf("締切時の部分的出力が失われました: %#v", response)
	}
}

func TestDockerTestExecutorStopsDrainingInFlightRequests(t *testing.T) {
	guard := newDockerTestGuard(t)
	started := make(chan struct{})
	run := func(ctx context.Context, _ string, _ []string, _ io.Writer, _ io.Writer) (int, error) {
		close(started)
		<-ctx.Done()
		return 1, ctx.Err()
	}
	executor := startTestDockerTestExecutor(t, guard, run)
	token := "5000000000000000000"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(token), stagingRequestBody(t, token, "./..."))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("owner実行が開始されませんでした")
	}

	stopDone := make(chan struct{})
	go func() {
		executor.stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
	case <-time.After(10 * time.Second):
		t.Fatal("executor停止がin-flight requestをdrainしませんでした")
	}
	response := waitStagingResponse(t, guard.dockerTestResponseDir, token)
	if response.ExitCode == 0 {
		t.Fatalf("取消されたrequestが成功として報告されました: %#v", response)
	}

	late := "5000000000000000001"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(late), stagingRequestBody(t, late, "./..."))
	time.Sleep(30 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(guard.dockerTestResponseDir, dockertest.StagingResponseFileName(late))); err == nil {
		t.Fatal("停止後のexecutorが新requestを実行しました")
	}
}

func TestDockerTestExecutorEnforcesRequestBudgetAndOutputCap(t *testing.T) {
	guard := newDockerTestGuard(t)
	guard.dockerTestMaxRequests = 1
	guard.dockerTestMaxOutputBytes = 32
	run := func(_ context.Context, _ string, _ []string, stdout, _ io.Writer) (int, error) {
		_, _ = io.WriteString(stdout, strings.Repeat("a", 64))
		return 0, nil
	}
	startTestDockerTestExecutor(t, guard, run)
	first := "6000000000000000000"
	second := "6000000000000000001"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(first), stagingRequestBody(t, first, "./..."))
	capped := waitStagingResponse(t, guard.dockerTestResponseDir, first)
	if capped.ExitCode != 1 || !strings.Contains(capped.Failure, "staging response上限") {
		t.Fatalf("出力上限超過がfail-closedになりません: %#v", capped)
	}
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(second), stagingRequestBody(t, second, "./..."))
	budget := waitStagingResponse(t, guard.dockerTestResponseDir, second)
	if budget.ExitCode != 1 || !strings.Contains(budget.Failure, "session上限") {
		t.Fatalf("request数上限がfail-closedになりません: %#v", budget)
	}
}

func TestDockerTestExecutorIgnoresDecoyRequestsPlantedDuringExecution(t *testing.T) {
	guard := newDockerTestGuard(t)
	recorder := &stagingRunRecorder{}
	stagingParent := filepath.Dir(guard.dockerTestStagingDir)
	holder := filepath.Join(stagingParent, "docker-test-held")
	dropped := filepath.Join(stagingParent, "docker-test-decoy-dropped")
	swapped := false
	run := func(_ context.Context, _ string, _ []string, _ io.Writer, _ io.Writer) (int, error) {
		if swapped {
			return 0, nil
		}
		swapped = true
		if err := os.Rename(guard.dockerTestStagingDir, holder); err != nil {
			return 1, err
		}
		if err := os.Mkdir(guard.dockerTestStagingDir, 0o700); err != nil {
			return 1, err
		}
		attacker := filepath.Join(guard.dockerTestStagingDir, dockertest.StagingRequestFileName("9300000000000000000"))
		if err := os.WriteFile(attacker, []byte(`{"schema_version":1,"token":"9300000000000000000","args":["./internal/app"]}`), 0o600); err != nil {
			return 1, err
		}
		if err := os.Rename(guard.dockerTestStagingDir, dropped); err != nil {
			return 1, err
		}
		if err := os.Rename(holder, guard.dockerTestStagingDir); err != nil {
			return 1, err
		}
		return 0, nil
	}
	legit := "9400000000000000000"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(legit), stagingRequestBody(t, legit, "./internal/runner"))
	startTestDockerTestExecutor(t, guard, run)

	response := waitStagingResponse(t, guard.dockerTestResponseDir, legit)
	if response.ExitCode != 0 {
		t.Fatalf("pinned directory内の正規requestが失敗しました: %#v", response)
	}
	followUp := "9400000000000000001"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(followUp), stagingRequestBody(t, followUp, "./internal/dockertest"))
	followUpResponse := waitStagingResponse(t, guard.dockerTestResponseDir, followUp)
	if followUpResponse.ExitCode != 0 {
		t.Fatalf("差替え復帰後のpinned directory内requestが失敗しました: %#v", followUpResponse)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(guard.dockerTestResponseDir, dockertest.StagingResponseFileName("9300000000000000000"))); err == nil {
		t.Fatal("処理中に差替え先へ植えられたrequestが実行されました")
	}
	for _, record := range recorder.snapshot() {
		if strings.Contains(strings.Join(record.args, " "), "./internal/app") {
			t.Fatalf("差替え先のrequest内容がowner実行へ到達しました: %#v", record)
		}
	}
}

func TestDockerTestExecutorKeepsPublishedResponseImmutable(t *testing.T) {
	guard := newDockerTestGuard(t)
	recorder := &stagingRunRecorder{}
	run := func(_ context.Context, root string, args []string, _ io.Writer, _ io.Writer) (int, error) {
		recorder.capture(root, args)
		return 0, nil
	}
	startTestDockerTestExecutor(t, guard, run)

	token := "9000000000000000000"
	published := `{"schema_version":1,"token":"9000000000000000000","exit_code":0,"stdout":"published\n","stderr":"","failure":""}`
	if err := os.WriteFile(filepath.Join(guard.dockerTestResponseDir, dockertest.StagingResponseFileName(token)), []byte(published), 0o600); err != nil {
		t.Fatal(err)
	}
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(token), stagingRequestBody(t, token, "./..."))
	waitStagingCondition(t, func() bool {
		return strings.Contains(dockerTestAttemptLogText(t, guard), "docker-test:response-exists:"+token)
	})
	data, err := os.ReadFile(filepath.Join(guard.dockerTestResponseDir, dockertest.StagingResponseFileName(token)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != published {
		t.Fatalf("既存responseが置き換えられました: %s", string(data))
	}
	entries, err := os.ReadDir(guard.dockerTestResponseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".response-") {
			t.Fatalf("response temp fileが作成されました: %s", entry.Name())
		}
	}
}

func TestDockerTestStagingClientRunsThroughExecutor(t *testing.T) {
	guard := newDockerTestGuard(t)
	recorder := &stagingRunRecorder{}
	codes := []int{0, 3}
	run := func(_ context.Context, root string, args []string, stdout, stderr io.Writer) (int, error) {
		call := recorder.capture(root, args)
		_, _ = io.WriteString(stdout, "container-test-output\n")
		_, _ = io.WriteString(stderr, "goquality-diagnostic {\"form\":\"diagnostic-go-test\",\"child_exit\":0,\"exit\":0}\n")
		return codes[call-1], nil
	}
	startTestDockerTestExecutor(t, guard, run)
	client := &dockertest.StagingClient{
		StagingDir:   guard.dockerTestStagingDir,
		ResponseDir:  guard.dockerTestResponseDir,
		PollInterval: 5 * time.Millisecond,
		Timeout:      5 * time.Second,
	}

	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./...", "-run", "^TestFocused$", "-count=1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("staging経由の診断 = code %d out %q err %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "container-test-output") || !strings.Contains(stderr.String(), "goquality-diagnostic") {
		t.Fatalf("staging経由の出力再現が壊れています out %q err %q", stdout.String(), stderr.String())
	}

	var retryStdout, retryStderr strings.Builder
	code = client.Run(context.Background(), []string{"./internal/app"}, &retryStdout, &retryStderr)
	if code != 3 {
		t.Fatalf("target終了codeの再現 = code %d out %q err %q", code, retryStdout.String(), retryStderr.String())
	}

	records := recorder.snapshot()
	if len(records) != 2 {
		t.Fatalf("owner実行の記録 = %#v", records)
	}
	if records[0].root != guard.repoRoot {
		t.Fatalf("実行root = %q want %q", records[0].root, guard.repoRoot)
	}
	wantArgs := []string{"./...", "-run", "^TestFocused$", "-count=1"}
	if strings.Join(records[0].args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("go test引数 = %#v want %#v", records[0].args, wantArgs)
	}
	entries, err := os.ReadDir(guard.dockerTestStagingDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "response-") {
			t.Fatalf("ownerがworker書込可能領域へresponseを書きました: %s", entry.Name())
		}
	}
}

func TestEncodeStagingResponseDisablesHTMLEscapingNearStreamCaps(t *testing.T) {
	output := strings.Repeat("<>&", dockerTestMaxOutputBytes/3)
	response := dockertest.StagingResponse{
		SchemaVersion: dockertest.StagingSchemaVersion,
		Token:         "8600000000000000000",
		ExitCode:      0,
		Stdout:        output,
		Stderr:        output,
	}
	data, err := encodeStagingResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > dockertest.StagingMaxResponseBytes || strings.Contains(string(data[:min(len(data), 2048)]), `\u003c`) {
		t.Fatalf("HTML-safe encodingが出力を増幅しました: encodedBytes=%d", len(data))
	}
	decoded, err := dockertest.ParseStagingResponse(data)
	if err != nil || decoded.ExitCode != 0 || decoded.Stdout != output || decoded.Stderr != output {
		t.Fatalf("上限付近の正当responseが再現されません: parseError=%v code=%d", err, decoded.ExitCode)
	}
}

func TestWriteStagingResponseBoundsEncodedExpansion(t *testing.T) {
	cases := []struct {
		name   string
		output string
	}{
		{name: "backslash-both-streams", output: strings.Repeat("\\", dockerTestMaxOutputBytes)},
		{name: "control-char-one-stream", output: strings.Repeat("\x00", 12<<20)},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := fmt.Sprintf("870000000000000000%d", i)
			response := dockertest.StagingResponse{
				SchemaVersion: dockertest.StagingSchemaVersion,
				Token:         token,
				ExitCode:      0,
				Stdout:        tc.output,
			}
			if i == 0 {
				response.Stderr = tc.output
			}
			dir := t.TempDir()
			if err := writeStagingResponse(dir, response); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, dockertest.StagingResponseFileName(token)))
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > dockertest.StagingMaxResponseBytes || len(data) >= 1024 {
				t.Fatalf("over-budget responseが小さなfailureに変換されません: %d bytes", len(data))
			}
			decoded, err := dockertest.ParseStagingResponse(data)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Token != token || decoded.ExitCode != 1 || !strings.Contains(decoded.Failure, "符号化後size上限") ||
				decoded.Stdout != "" || decoded.Stderr != "" {
				t.Fatalf("over-budget responseのfail-closed = %#v", decoded)
			}
		})
	}
}

func TestEncodeStagingResponseExactByteBudget(t *testing.T) {
	token := "8800000000000000000"
	empty := dockertest.StagingResponse{SchemaVersion: dockertest.StagingSchemaVersion, Token: token, ExitCode: 0}
	header, err := encodeStagingResponse(empty)
	if err != nil {
		t.Fatal(err)
	}
	length := dockertest.StagingMaxResponseBytes - len(header)
	atLimit := empty
	atLimit.Stdout = strings.Repeat("x", length)
	data, err := encodeStagingResponse(atLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != dockertest.StagingMaxResponseBytes {
		t.Fatalf("上限ちょうどのresponseが保存されません: %d want %d", len(data), dockertest.StagingMaxResponseBytes)
	}
	overLimit := atLimit
	overLimit.Stdout += "x"
	data, err = encodeStagingResponse(overLimit)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := dockertest.ParseStagingResponse(data)
	if err != nil || decoded.ExitCode != 1 || decoded.Stdout != "" || !strings.Contains(decoded.Failure, "符号化後size上限") {
		t.Fatalf("上限+1 byteの応答 = %#v err=%v", decoded, err)
	}
}

func TestDockerTestExecutorEncodedOverflowIsClientReadable(t *testing.T) {
	guard := newDockerTestGuard(t)
	run := func(_ context.Context, _ string, _ []string, stdout, stderr io.Writer) (int, error) {
		output := strings.Repeat("\\", dockerTestMaxOutputBytes)
		_, _ = io.WriteString(stdout, output)
		_, _ = io.WriteString(stderr, output)
		return 0, nil
	}
	startTestDockerTestExecutor(t, guard, run)
	client := &dockertest.StagingClient{
		StagingDir:   guard.dockerTestStagingDir,
		ResponseDir:  guard.dockerTestResponseDir,
		PollInterval: 5 * time.Millisecond,
		Timeout:      15 * time.Second,
	}
	var stdout, stderr strings.Builder
	code := client.Run(context.Background(), []string{"./..."}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "符号化後size上限") || strings.Contains(stderr.String(), "締切内") {
		t.Fatalf("owner発行の過大responseがclientで失敗になりません: code %d out %q err %q", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("過大responseからのstdoutが成功内容として出力されました: %d bytes", stdout.Len())
	}
}
