package observationexec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type GoTestInput struct {
	ModuleDir   string
	ArtifactDir string
	ExecutionID string
	Race        bool
	DeadlineMS  int64
}

type GoTestOutcome struct {
	Status     string
	ExitCode   int
	ExitSource string
	Detail     string
	LogPath    string
	DurationMS int64
}

type moduleTreeCopier struct {
	files int64
	bytes int64
}

type isolatedGoTestRun struct {
	outcome GoTestOutcome
	gateLog []byte
}

const (
	StatusPass = "pass"
	StatusFail = "fail"

	exitSourceTarget       = "target"
	exitSourceDeadline     = "deadline"
	exitSourceWrapper      = "wrapper"
	exitSourceInputInvalid = "input"
	exitSourceConfinement  = "confinement"

	copyMaxFiles  = 200_000
	copyMaxBytes  = 1 << 29
	logMaxBytes   = 1 << 20
	copySkipGit   = ".git"
	deadlineGrace = 5 * time.Second
)

var observationConfinementAdmission = ConfinementAdmission

func RunIsolatedGoTest(input GoTestInput) GoTestOutcome {
	if err := ValidateExecutionID(input.ExecutionID); err != nil {
		return goTestInputFailure(err)
	}
	if err := observationConfinementAdmission(); err != nil {
		return goTestConfinementFailure(err)
	}
	started := time.Now()
	tempRoot, err := os.MkdirTemp("", "glm-worker-observation-")
	if err != nil {
		return goTestWrapperFailure(started, fmt.Sprintf("隔離temp rootを作成できません: %v", err))
	}
	defer func() { _ = os.RemoveAll(tempRoot) }()
	if err := ConfinementPreflight(tempRoot); err != nil {
		outcome := goTestConfinementFailure(err)
		outcome.DurationMS = time.Since(started).Milliseconds()
		return outcome
	}
	if err := prepareIsolatedGoTestRoot(tempRoot, input.ModuleDir); err != nil {
		return goTestInputFailure(err)
	}
	run := runIsolatedGoTestProcess(input, tempRoot)
	run.outcome.DurationMS = time.Since(started).Milliseconds()
	return finalizeIsolatedGoTestOutcome(input, run)
}

func prepareIsolatedGoTestRoot(tempRoot string, moduleDir string) error {
	if err := copyModuleTree(filepath.Join(tempRoot, "input"), moduleDir); err != nil {
		return err
	}
	for _, bounded := range []string{filepath.Join(tempRoot, "tmp"), filepath.Join(tempRoot, "cache")} {
		if err := os.MkdirAll(bounded, 0o700); err != nil {
			return fmt.Errorf("隔離実行のbounded dirを作成できません: %w", err)
		}
	}
	return nil
}

func runIsolatedGoTestProcess(input GoTestInput, tempRoot string) isolatedGoTestRun {
	launchArgs, err := confinedLaunchArgs(tempRoot, append([]string{"go"}, isolatedGoTestArgs(input.Race)...))
	if err != nil {
		return isolatedGoTestRun{outcome: goTestConfinementFailure(err)}
	}
	command := newObservationProcessGroupCmd(launchArgs[0], launchArgs[1:]...)
	command.Dir = filepath.Join(tempRoot, "input")
	command.Env = isolatedGoTestEnv(tempRoot)
	var gateLog bytes.Buffer
	command.Stdout = &gateLog
	command.Stderr = &gateLog
	if err := command.Start(); err != nil {
		return isolatedGoTestRun{outcome: goTestStartFailure(err), gateLog: gateLog.Bytes()}
	}
	deadline := time.Duration(input.ResolvedGoTestDeadlineMS()) * time.Millisecond
	deadlineAt := time.Now().Add(deadline)
	timer := time.AfterFunc(deadline, func() { terminateObservationProcessGroup(command.Process.Pid) })
	runErr := command.Wait()
	timer.Stop()
	return isolatedGoTestRun{outcome: classifyIsolatedGoTestOutcome(runErr, deadlineAt, deadline), gateLog: gateLog.Bytes()}
}

func isolatedGoTestArgs(race bool) []string {
	args := []string{"test", "./..."}
	if race {
		args = append(args, "-race")
	}
	return args
}

func classifyIsolatedGoTestOutcome(runErr error, deadlineAt time.Time, deadline time.Duration) GoTestOutcome {
	if runErr == nil {
		return GoTestOutcome{Status: StatusPass, ExitCode: 0, ExitSource: exitSourceTarget}
	}
	outcome := GoTestOutcome{Status: StatusFail, ExitCode: goTestExitCode(runErr), ExitSource: exitSourceTarget}
	if !time.Now().Before(deadlineAt) {
		outcome.ExitSource = exitSourceDeadline
		outcome.Detail = fmt.Sprintf("deadline(%s)を超過したため隔離process groupを終了しました", deadline)
	}
	return outcome
}

func goTestStartFailure(err error) GoTestOutcome {
	return GoTestOutcome{Status: StatusFail, ExitCode: 1, ExitSource: exitSourceWrapper, Detail: boundedDetail(fmt.Sprintf("隔離go testを開始できません: %v", err))}
}

func finalizeIsolatedGoTestOutcome(input GoTestInput, run isolatedGoTestRun) GoTestOutcome {
	logPath, logErr := writeGoTestLog(input.ArtifactDir, input.ExecutionID, run.gateLog)
	if logErr != nil {
		run.outcome.Status = StatusFail
		if run.outcome.ExitCode == 0 && run.outcome.ExitSource == exitSourceTarget {
			run.outcome.ExitCode = 1
			run.outcome.ExitSource = exitSourceWrapper
		}
		run.outcome.Detail = strings.TrimSpace(run.outcome.Detail + "; gate logを保存できません: " + logErr.Error())
		return run.outcome
	}
	run.outcome.LogPath = logPath
	return run.outcome
}

func (input GoTestInput) ResolvedGoTestDeadlineMS() int64 {
	if input.DeadlineMS > 0 {
		return input.DeadlineMS
	}
	return DefaultDeadlineMS
}

func goTestExitCode(runErr error) int {
	exitErr := &exec.ExitError{}
	ok := errors.As(runErr, &exitErr)
	if !ok {
		return 1
	}
	if code := exitErr.ExitCode(); code >= 0 {
		return code
	}
	return 1
}

func goTestInputFailure(err error) GoTestOutcome {
	return GoTestOutcome{Status: StatusFail, ExitCode: 1, ExitSource: exitSourceInputInvalid, Detail: boundedDetail(err.Error())}
}

func goTestConfinementFailure(err error) GoTestOutcome {
	return GoTestOutcome{Status: StatusFail, ExitCode: 1, ExitSource: exitSourceConfinement, Detail: boundedDetail(err.Error())}
}

func goTestWrapperFailure(started time.Time, reason string) GoTestOutcome {
	return GoTestOutcome{Status: StatusFail, ExitCode: 1, ExitSource: exitSourceWrapper, Detail: boundedDetail(reason), DurationMS: time.Since(started).Milliseconds()}
}

func copyModuleTree(destination string, source string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("隔離入力のmodule dirを確認できません: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("隔離入力のmodule dir %sがdirectoryではありません", source)
	}
	copier := &moduleTreeCopier{files: 1, bytes: 0}
	return copier.copyDir(destination, source)
}

func (c *moduleTreeCopier) copyDir(destination string, source string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("隔離入力dirを作成できません: %w", err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("隔離入力dirを読めません: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == copySkipGit && entry.IsDir() {
			continue
		}
		if err := c.copyEntry(destination, source, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (c *moduleTreeCopier) copyEntry(destination string, source string, name string) error {
	sourcePath := filepath.Join(source, name)
	destinationPath := filepath.Join(destination, name)
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("隔離入力を確認できません: %w", err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("隔離入力にsymlink %sがありました。境界を保つためcopyできません", sourcePath)
	case info.IsDir():
		return c.copyDir(destinationPath, sourcePath)
	case info.Mode().IsRegular():
		return c.copyFile(destinationPath, sourcePath, info.Mode().Perm())
	default:
		return fmt.Errorf("隔離入力に通常file以外のentry %sがありました", sourcePath)
	}
}

func (c *moduleTreeCopier) copyFile(destination string, source string, perm fs.FileMode) error {
	c.files++
	if c.files > copyMaxFiles {
		return fmt.Errorf("隔離入力のfile数が上限(%d)を超えました", copyMaxFiles)
	}
	if c.bytes > copyMaxBytes {
		return fmt.Errorf("隔離入力の合計sizeが上限(%d bytes)を超えました", copyMaxBytes)
	}
	src, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("隔離入力fileを読めません: %w", err)
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("隔離入力のcopy先を作れません: %w", err)
	}
	written, err := io.Copy(dst, src)
	closeErr := dst.Close()
	if err != nil {
		return fmt.Errorf("隔離入力fileをcopyできません: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("隔離入力fileを保存できません: %w", closeErr)
	}
	c.bytes += written
	return nil
}

func isolatedGoTestEnv(tempRoot string) []string {
	allowed := []string{"PATH", "HOME", "TZ", "LANG", "LC_ALL", "LC_CTYPE"}
	env := make([]string, 0, len(allowed)+8)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		for _, candidate := range allowed {
			if name == candidate {
				env = append(env, entry)
			}
		}
	}
	return append(env,
		"TMPDIR="+filepath.Join(tempRoot, "tmp"),
		"GOTMPDIR="+filepath.Join(tempRoot, "tmp"),
		"GOCACHE="+filepath.Join(tempRoot, "cache"),
		"GOFLAGS=-mod=readonly",
		"GOPROXY=off",
		"GOENV=off",
		"GOTOOLCHAIN=local",
	)
}

func writeGoTestLog(artifactDir string, executionID string, data []byte) (string, error) {
	dir := filepath.Join(artifactDir, "observation-exec", executionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("observation実行artifact dirを作成できません: %w", err)
	}
	path := filepath.Join(dir, "go-test.log")
	if err := os.WriteFile(path, boundedLog(data), 0o600); err != nil {
		return "", fmt.Errorf("observation実行logを保存できません: %w", err)
	}
	return path, nil
}

func boundedLog(data []byte) []byte {
	if len(data) <= logMaxBytes {
		return data
	}
	return data[len(data)-logMaxBytes:]
}

func boundedDetail(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > 4096 {
		return reason[:4096]
	}
	return reason
}
