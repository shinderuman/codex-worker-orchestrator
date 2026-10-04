package observationexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	files    int64
	bytes    int64
	maxBytes int64
}

type isolatedGoTestRun struct {
	outcome      GoTestOutcome
	gateLog      []byte
	gateLogTotal int64
	logTruncated bool
}

type boundedTailBuffer struct {
	mu    sync.Mutex
	data  []byte
	total int64
	limit int
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

func RunIsolatedGoTest(ctx context.Context, input GoTestInput) GoTestOutcome {
	return runIsolatedGoTest(ctx, input)
}

func prepareIsolatedGoTestRoot(tempRoot string, moduleDir string) error {
	if err := copyModuleTree(filepath.Join(tempRoot, "input"), moduleDir); err != nil {
		return err
	}
	for _, bounded := range []string{filepath.Join(tempRoot, "tmp"), filepath.Join(tempRoot, "cache"), filepath.Join(tempRoot, "home")} {
		if err := os.MkdirAll(bounded, 0o700); err != nil {
			return fmt.Errorf("隔離実行のbounded dirを作成できません: %w", err)
		}
	}
	return nil
}

func (b *boundedTailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	b.total += int64(written)
	if b.limit <= 0 || written == 0 {
		return written, nil
	}
	if written >= b.limit {
		if cap(b.data) < b.limit {
			b.data = make([]byte, b.limit)
		} else {
			b.data = b.data[:b.limit]
		}
		copy(b.data, p[written-b.limit:])
		return written, nil
	}
	if overflow := len(b.data) + written - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return written, nil
}

func (b *boundedTailBuffer) snapshot() ([]byte, int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := append([]byte(nil), b.data...)
	return data, b.total, b.total > int64(len(b.data))
}

func capturedIsolatedGoTestRun(outcome GoTestOutcome, gateLog *boundedTailBuffer) isolatedGoTestRun {
	data, total, truncated := gateLog.snapshot()
	return isolatedGoTestRun{outcome: outcome, gateLog: data, gateLogTotal: total, logTruncated: truncated}
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

func goTestDeadlineOutcome(runErr error, deadline time.Duration) GoTestOutcome {
	exitCode := 1
	if runErr != nil {
		exitCode = goTestExitCode(runErr)
	}
	return GoTestOutcome{
		Status:     StatusFail,
		ExitCode:   exitCode,
		ExitSource: exitSourceDeadline,
		Detail:     fmt.Sprintf("deadline(%s)を超過したため隔離process groupを終了しました", deadline),
	}
}

func goTestStartFailure(err error) GoTestOutcome {
	return GoTestOutcome{Status: StatusFail, ExitCode: 1, ExitSource: exitSourceWrapper, Detail: boundedDetail(fmt.Sprintf("隔離go testを開始できません: %v", err))}
}

func finalizeIsolatedGoTestOutcome(input GoTestInput, run isolatedGoTestRun) GoTestOutcome {
	logPath, logErr := writeGoTestLog(input.ArtifactDir, input.ExecutionID, run.gateLog, run.gateLogTotal, run.logTruncated)
	if logErr != nil {
		run.outcome.Status = StatusFail
		if run.outcome.ExitCode == 0 && run.outcome.ExitSource == exitSourceTarget {
			run.outcome.ExitCode = 1
			run.outcome.ExitSource = exitSourceWrapper
		}
		run.outcome.Detail = strings.TrimSpace(run.outcome.Detail + "; gate logを保存できません: " + logErr.Error())
		return run.outcome
	}
	if run.logTruncated {
		run.outcome.Detail = strings.TrimSpace(run.outcome.Detail + fmt.Sprintf("; gate logは末尾%d bytesへ切り詰めました(total=%d)", len(run.gateLog), run.gateLogTotal))
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
	copier := &moduleTreeCopier{files: 1, maxBytes: copyMaxBytes}
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
	limit := c.maxBytes
	if limit <= 0 {
		limit = copyMaxBytes
	}
	remaining := limit - c.bytes
	if remaining < 0 {
		return fmt.Errorf("隔離入力の合計sizeが上限(%d bytes)を超えました", limit)
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
	committed := false
	defer func() {
		_ = dst.Close()
		if !committed {
			_ = os.Remove(destination)
		}
	}()
	written, err := io.Copy(dst, io.LimitReader(src, remaining))
	if err != nil {
		return fmt.Errorf("隔離入力fileをcopyできません: %w", err)
	}
	var extra [1]byte
	extraN, extraErr := src.Read(extra[:])
	if extraN > 0 {
		return fmt.Errorf("隔離入力の合計sizeが上限(%d bytes)を超えました", limit)
	}
	if extraErr != nil && !errors.Is(extraErr, io.EOF) {
		return fmt.Errorf("隔離入力fileのsize境界を確認できません: %w", extraErr)
	}
	if err := dst.Close(); err != nil {
		return fmt.Errorf("隔離入力fileを保存できません: %w", err)
	}
	c.bytes += written
	committed = true
	return nil
}

func writeGoTestLog(artifactDir string, executionID string, data []byte, total int64, truncated bool) (string, error) {
	dir := filepath.Join(artifactDir, "observation-exec", executionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("observation実行artifact dirを作成できません: %w", err)
	}
	path := filepath.Join(dir, "go-test.log")
	if err := os.WriteFile(path, durableBoundedLog(data, total, truncated), 0o600); err != nil {
		return "", fmt.Errorf("observation実行logを保存できません: %w", err)
	}
	return path, nil
}

func durableBoundedLog(data []byte, total int64, truncated bool) []byte {
	if !truncated {
		return append([]byte(nil), data...)
	}
	header := []byte(fmt.Sprintf("[observation output truncated: retained final bytes; total=%d]\n", total))
	keep := logMaxBytes - len(header)
	if keep < 0 {
		keep = 0
	}
	if len(data) > keep {
		data = data[len(data)-keep:]
	}
	result := make([]byte, 0, len(header)+len(data))
	result = append(result, header...)
	return append(result, data...)
}

func boundedDetail(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > 4096 {
		return reason[:4096]
	}
	return reason
}
