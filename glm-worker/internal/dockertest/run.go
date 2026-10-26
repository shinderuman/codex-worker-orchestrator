package dockertest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type serverInfo struct {
	OSType       string
	Architecture string
	NCPU         int
	MemTotal     int64
}

type diagnostic struct {
	TargetExitCode *int             `json:"target_exit_code,omitempty"`
	Form           string           `json:"form"`
	Status         string           `json:"status"`
	RequiredGo     string           `json:"required_go_version,omitempty"`
	Head           string           `json:"head,omitempty"`
	IndexDigest    string           `json:"index_digest,omitempty"`
	WorktreeDigest string           `json:"worktree_digest,omitempty"`
	ArchiveSHA256  string           `json:"archive_sha256,omitempty"`
	ImageID        string           `json:"image_id,omitempty"`
	GitVersion     string           `json:"git_version,omitempty"`
	Platform       string           `json:"platform"`
	CPUs           int              `json:"cpus,omitempty"`
	MemoryBytes    int64            `json:"memory_bytes,omitempty"`
	CacheVolume    string           `json:"cache_volume,omitempty"`
	Arguments      []string         `json:"argv"`
	SourceChanged  *bool            `json:"source_changed,omitempty"`
	IgnoredAbsent  *bool            `json:"ignored_inputs_absent,omitempty"`
	PreparationMS  int64            `json:"preparation_ms"`
	TestMS         int64            `json:"test_ms"`
	TotalMS        int64            `json:"total_ms"`
	PhasesMS       map[string]int64 `json:"phases_ms"`
	ExitCode       int              `json:"exit"`
	Failure        string           `json:"failure,omitempty"`
}

type execution struct {
	ctx       context.Context
	root      string
	temporary string
	container string
	snapshot  state.GitSnapshot
	detail    *diagnostic
	stdout    io.Writer
	stderr    io.Writer
}

const platformARM64 = "arm64"
const platformLinux = "linux"

var errSourceChanged = errors.New("Docker診断中にsourceが変化しました")

func Run(ctx context.Context, repository string, args []string, stdout, stderr io.Writer) (code int, err error) {
	started := time.Now()
	if len(args) == 0 {
		args = []string{"./..."}
	}
	d := diagnostic{Form: "diagnostic-go-test-linux", Platform: "linux/arm64", Arguments: args, PhasesMS: map[string]int64{}}
	x := execution{ctx: ctx, detail: &d, stdout: stdout, stderr: stderr}
	defer func() {
		cleanupStarted := time.Now()
		cleanupErr := x.cleanup()
		d.PhasesMS["cleanup"] = time.Since(cleanupStarted).Milliseconds()
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
			if code == 0 {
				code = 1
			}
		}
		code, err = finishDiagnostic(&d, started, code, err, stderr)
	}()
	if err := x.configure(repository); err != nil {
		return 1, err
	}
	if err := x.prepare(); err != nil {
		return 1, err
	}
	return x.test(args)
}

func finishDiagnostic(d *diagnostic, started time.Time, code int, resultErr error, stderr io.Writer) (int, error) {
	d.TotalMS = time.Since(started).Milliseconds()
	d.PreparationMS = d.TotalMS - d.TestMS - d.PhasesMS["source_check"] - d.PhasesMS["cleanup"]
	d.ExitCode, d.Status = code, "fail"
	if code == 0 && resultErr == nil {
		d.Status = "pass"
	}
	if resultErr != nil {
		d.Failure = resultErr.Error()
	}
	body, err := json.Marshal(d)
	if err == nil {
		_, err = fmt.Fprintln(stderr, "goquality-linux-diagnostic", string(body))
	}
	if err != nil && code == 0 {
		return 1, err
	}
	return code, resultErr
}

func (x *execution) configure(repository string) error {
	root, err := filepath.Abs(repository)
	if err != nil {
		return err
	}
	x.root = root
	x.snapshot, err = state.CaptureGitSnapshot(root)
	if err != nil {
		return err
	}
	d := x.detail
	d.Head, d.IndexDigest, d.WorktreeDigest = x.snapshot.Head, x.snapshot.IndexDigest, x.snapshot.WorktreeDigest
	ignoredAbsent, err := qualitygate.NoIgnoredInputs(root)
	if err != nil {
		return err
	}
	d.IgnoredAbsent = &ignoredAbsent
	if !ignoredAbsent {
		return fmt.Errorf("ignored inputがあるためsource同一性を保証できません")
	}
	d.RequiredGo, err = qualitygate.RequiredGoVersion(root)
	if err != nil {
		return err
	}
	server, err := localServer(x.ctx)
	if err != nil {
		return err
	}
	d.CPUs, d.MemoryBytes = server.NCPU, server.MemTotal
	x.temporary, err = os.MkdirTemp("", "codex-go-linux-")
	return err
}

func (x *execution) prepare() error {
	d := x.detail
	lintVersion, err := lintGoVersion(x.root)
	if err != nil {
		return err
	}
	phase := time.Now()
	d.ImageID, err = buildImage(x.ctx, x.root, x.temporary, d.RequiredGo, lintVersion, x.stderr)
	d.PhasesMS["image"] = time.Since(phase).Milliseconds()
	if err != nil {
		return err
	}
	phase = time.Now()
	archive, digest, err := sourceArchive(x.ctx, x.root, x.temporary, x.snapshot)
	d.PhasesMS["source_export"] = time.Since(phase).Milliseconds()
	if err != nil {
		return err
	}
	d.ArchiveSHA256 = digest
	if err := unchangedSource(x.root, x.snapshot); err != nil {
		return err
	}
	d.CacheVolume = "codex-worker-orchestrator-go-test-" + d.RequiredGo + "-linux-arm64"
	x.container, err = createContainer(x.ctx, d.ImageID, d.CacheVolume, d.RequiredGo, d.CPUs)
	if err != nil {
		return err
	}
	phase = time.Now()
	if err := populateContainer(x.ctx, x.root, x.container, archive, digest); err != nil {
		return err
	}
	d.PhasesMS["source_transfer"] = time.Since(phase).Milliseconds()
	if err := verifyToolchain(x.ctx, x.root, x.container, d.RequiredGo); err != nil {
		return err
	}
	gitVersion, err := output(x.ctx, x.root, "docker", "exec", "--user", "worker", x.container, "git", "--version")
	d.GitVersion = strings.TrimSpace(string(gitVersion))
	return err
}

func (x *execution) test(args []string) (int, error) {
	commandArgs := append([]string{"exec", "--user", "worker", "--workdir", "/workspace", x.container, "./goquality", "test"}, args...)
	runErr := x.runTestCommand(commandArgs)
	code := exitCode(runErr)
	changedErr := x.checkSourceAfterTest()
	if changedErr != nil && code == 0 {
		return 1, changedErr
	}
	return code, errors.Join(runErr, changedErr)
}

func (x *execution) runTestCommand(args []string) error {
	command := exec.CommandContext(x.ctx, "docker", args...)
	command.Stdout, command.Stderr = x.stdout, x.stderr
	started := time.Now()
	err := command.Run()
	x.detail.TestMS = time.Since(started).Milliseconds()
	return err
}

func (x *execution) checkSourceAfterTest() error {
	checkStarted := time.Now()
	changedErr := unchangedSource(x.root, x.snapshot)
	x.detail.PhasesMS["source_check"] = time.Since(checkStarted).Milliseconds()
	if changedErr == nil || errors.Is(changedErr, errSourceChanged) {
		changed := changedErr != nil
		x.detail.SourceChanged = &changed
	}
	return changedErr
}

func (x *execution) cleanup() error {
	var result error
	if x.container != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := output(ctx, x.root, "docker", "rm", "--force", x.container); err != nil {
			result = fmt.Errorf("実験containerのcleanup失敗: %w", err)
		}
	}
	if x.temporary != "" {
		if err := os.RemoveAll(x.temporary); err != nil {
			result = errors.Join(result, fmt.Errorf("一時sourceのcleanup失敗: %w", err))
		}
	}
	return result
}

func unchangedSource(root string, before state.GitSnapshot) error {
	after, err := state.CaptureGitSnapshot(root)
	if err != nil {
		return fmt.Errorf("実行後sourceを確認できません: %w", err)
	}
	if before.Head != after.Head || before.IndexDigest != after.IndexDigest || before.WorktreeDigest != after.WorktreeDigest {
		return errSourceChanged
	}
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode()
	}
	return 1
}

func output(ctx context.Context, directory, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	data, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %v: %w: %s", name, args, err, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func localServer(ctx context.Context) (serverInfo, error) {
	var server serverInfo
	if host := os.Getenv("DOCKER_HOST"); host != "" && !strings.HasPrefix(host, "unix://") {
		return server, fmt.Errorf("ローカルDockerのUnix socketが必要です")
	}
	endpoint, err := output(ctx, "", "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	if err != nil {
		return server, err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(endpoint)), "unix://") {
		return server, fmt.Errorf("remote Docker contextは診断対象外です")
	}
	data, err := output(ctx, "", "docker", "info", "--format", "{{json .}}")
	if err != nil {
		return server, err
	}
	if err := json.Unmarshal(data, &server); err != nil {
		return server, err
	}
	if server.OSType != platformLinux || (server.Architecture != "aarch64" && server.Architecture != platformARM64) || server.NCPU < 1 {
		return server, fmt.Errorf("native Linux ARM64 Dockerが必要です: os=%s arch=%s cpus=%d", server.OSType, server.Architecture, server.NCPU)
	}
	return server, nil
}

func lintGoVersion(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, qualitygate.QualityToolsFile))
	if err != nil {
		return "", err
	}
	pattern := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if ok && key == "lint-go" && pattern.MatchString(value) {
			return value, nil
		}
	}
	return "", fmt.Errorf("quality-tools.ymlのlint-go versionが不正です")
}

func buildImage(ctx context.Context, root, temporary, version, lintVersion string, stderr io.Writer) (string, error) {
	buildContext := filepath.Join(temporary, "image-context")
	if err := os.Mkdir(buildContext, 0o700); err != nil {
		return "", err
	}
	for _, name := range []string{qualitygate.QualityToolsFile, "install-quality-tools.sh"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(buildContext, name), data, 0600); err != nil {
			return "", err
		}
	}
	tag := "codex-worker-orchestrator-go-test:go" + version + "-lint" + lintVersion + "-arm64"
	command := exec.CommandContext(ctx, "docker", "build", "--quiet", "--platform", "linux/arm64", "--build-arg", "GO_VERSION="+version, "--build-arg", "LINT_GO_VERSION="+lintVersion, "--file", filepath.Join(root, "tools", "go-test.Dockerfile"), "--tag", tag, buildContext)
	command.Stdout, command.Stderr = stderr, stderr
	if err := command.Run(); err != nil {
		return "", err
	}
	data, err := output(ctx, root, "docker", "image", "inspect", tag, "--format", "{{.Id}}")
	return strings.TrimSpace(string(data)), err
}

func createContainer(ctx context.Context, image, cache, version string, cpus int) (string, error) {
	args := []string{"create", "--init", "--platform", "linux/arm64", "--mount", "type=volume,source=" + cache + ",target=/home/worker/.cache/go-build", "--env", "GOTOOLCHAIN=go" + version, "--env", "GOMAXPROCS=" + strconv.Itoa(cpus), image}
	data, err := output(ctx, "", "docker", args...)
	return strings.TrimSpace(string(data)), err
}

func populateContainer(ctx context.Context, root, container, archive, digest string) error {
	for _, args := range [][]string{
		{"cp", archive, container + ":/tmp/source.tar"},
		{"start", container},
		{"exec", container, "sha256sum", "/tmp/source.tar"},
	} {
		data, err := output(ctx, root, "docker", args...)
		if err != nil {
			return err
		}
		if args[0] == "exec" && (len(strings.Fields(string(data))) == 0 || strings.Fields(string(data))[0] != digest) {
			return fmt.Errorf("containerへ転送したsource archiveのSHA-256が不一致です")
		}
	}
	_, err := output(ctx, root, "docker", "exec", container, "sh", "-c", "tar -xf /tmp/source.tar -C /workspace && chown -R worker:worker /workspace /home/worker/.cache/go-build")
	return err
}

func verifyToolchain(ctx context.Context, root, container, required string) error {
	data, err := output(ctx, root, "docker", "exec", "--user", "worker", "--workdir", "/workspace", container, "go", "env", "-json", "GOVERSION", "GOOS", "GOARCH")
	if err != nil {
		return err
	}
	var observed map[string]string
	if err := json.Unmarshal(data, &observed); err != nil {
		return err
	}
	if observed["GOVERSION"] != "go"+required || observed["GOOS"] != platformLinux || observed["GOARCH"] != platformARM64 {
		return fmt.Errorf("containerの実toolchainが要求と不一致です: %v", observed)
	}
	return nil
}
