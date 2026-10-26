package dockertest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"io"
	"strconv"
	"strings"
	"time"
)

func RunCanonical(ctx context.Context, repository string, goArgs []string, stdout, stderr io.Writer) (toolchain *qualitygate.RunToolchain, evidence *qualitygate.DockerExecution, resultErr error) {
	started := time.Now()
	d := diagnostic{Form: "canonical-go-test-linux", Platform: "linux/arm64", Arguments: goArgs, PhasesMS: map[string]int64{}}
	x := execution{ctx: ctx, detail: &d, stdout: stdout, stderr: stderr}
	code := 1
	defer func() {
		cleanupStarted := time.Now()
		cleanupErr := x.cleanup()
		d.PhasesMS["cleanup"] = time.Since(cleanupStarted).Milliseconds()
		if cleanupErr != nil {
			resultErr = errors.Join(resultErr, cleanupErr)
			if code == 0 {
				code = 1
			}
		}
		code, resultErr = finishDiagnostic(&d, started, code, resultErr, io.Discard)
		evidence = canonicalEvidence(d)
	}()
	if len(goArgs) < 2 || goArgs[0] != "test" {
		return nil, nil, fmt.Errorf("canonical Docker execution requires fixed Go test arguments")
	}
	if err := x.configure(repository); err != nil {
		return nil, nil, err
	}
	if err := x.prepare(); err != nil {
		return nil, nil, err
	}
	toolchain, resultErr = x.captureCanonicalToolchain(goArgs)
	if resultErr != nil {
		return toolchain, nil, resultErr
	}
	code, resultErr = x.testCanonical(goArgs)
	if code != 0 && resultErr == nil {
		resultErr = fmt.Errorf("正規Docker実行が失敗しました: exit %d", code)
	}
	return toolchain, nil, resultErr
}

func (x *execution) captureCanonicalToolchain(args []string) (*qualitygate.RunToolchain, error) {
	data, err := output(x.ctx, x.root, "docker", "exec", "--user", "worker", "--workdir", "/workspace", x.container, "go", "env", "-json", "GOVERSION", "GOOS", "GOARCH", "GOCACHE", "GOFLAGS")
	if err != nil {
		return nil, err
	}
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	flags, recorded := env["GOFLAGS"]
	t := &qualitygate.RunToolchain{Backend: qualitygate.BackendDockerARM64, GoVersion: env["GOVERSION"], RequiredGoVersion: x.detail.RequiredGo, GOOS: env["GOOS"], GOARCH: env["GOARCH"], GoCache: env["GOCACHE"], GoFlags: flags, EnvironmentRecorded: recorded, Parallelism: x.detail.CPUs, Command: "go " + strings.Join(args, " "), TestCache: "unknown"}
	if !t.MatchesRequiredGoVersion() || t.GOOS != platformLinux || t.GOARCH != platformARM64 || t.GoFlags != "" || t.GoCache == "" {
		return t, fmt.Errorf("canonical container toolchain does not match the required execution")
	}
	return t, nil
}

func canonicalEvidence(d diagnostic) *qualitygate.DockerExecution {
	return &qualitygate.DockerExecution{TargetExitCode: d.TargetExitCode, ImageID: d.ImageID, GitVersion: d.GitVersion, ArchiveSHA256: d.ArchiveSHA256, SourceHead: d.Head, IndexDigest: d.IndexDigest, WorktreeDigest: d.WorktreeDigest, CacheVolume: d.CacheVolume, CPUs: d.CPUs, MemoryBytes: d.MemoryBytes, PreparationMS: d.PreparationMS, TestMS: d.TestMS, TotalMS: d.TotalMS, PhasesMS: d.PhasesMS, SourceChanged: d.SourceChanged, Status: d.Status}
}

func (x *execution) testCanonical(args []string) (int, error) {
	started := time.Now()
	script := `go "$@"; result=$?; printf '%s\n' "$result" > /tmp/canonical-go-exit; exit "$result"`
	command := append([]string{"exec", "--user", "worker", "--workdir", "/workspace/glm-worker", x.container, "sh", "-c", script, "canonical-go"}, args...)
	proxyErr := x.runTestCommand(command)
	code, targetErr := x.attestTargetExit(proxyErr)
	x.detail.TestMS = time.Since(started).Milliseconds()
	sourceErr := x.checkSourceAfterTest()
	if sourceErr != nil && code == 0 {
		code = 1
	}
	return code, errors.Join(targetErr, sourceErr)
}

func (x *execution) attestTargetExit(proxyErr error) (int, error) {
	data, err := output(x.ctx, x.root, "docker", "exec", "--user", "worker", x.container, "cat", "/tmp/canonical-go-exit")
	if err != nil {
		return 1, fmt.Errorf("go target終了証拠を取得できません: %w", err)
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || code < 0 || code > 255 {
		return 1, fmt.Errorf("go target終了証拠が不正です")
	}
	x.detail.TargetExitCode = &code
	if exitCode(proxyErr) != code {
		return 1, fmt.Errorf("docker proxyとGo targetの終了値が一致しません: proxy=%w target=%d", proxyErr, code)
	}
	if code == 126 || code == 127 {
		return 1, fmt.Errorf("container内のGo targetを起動できません: exit %d", code)
	}
	if code != 0 {
		return code, &qualitygate.TargetExitError{Code: code}
	}
	return 0, nil
}
