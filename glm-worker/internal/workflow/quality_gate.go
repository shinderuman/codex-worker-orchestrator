package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const qualitySurfaceBaselineStateKey = "quality-surface-baseline"

func runRepositoryQualityGate(root string) (harnesslint.Report, error) {
	if root == "" || !harnesslint.AppliesTo(root) {
		return harnesslint.Report{Status: "pass", Violations: []harnesslint.Violation{}}, nil
	}
	if _, err := harnesslint.Run(root, true); err != nil {
		return harnesslint.Report{}, err
	}
	return harnesslint.Check(root)
}

func captureQualitySurfaceDigest(root string) (string, error) {
	if root == "" || !harnesslint.AppliesTo(root) {
		return "", nil
	}
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	data, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("list quality surface: %w", err)
	}
	paths := splitNul(data)
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		path = filepath.ToSlash(path)
		if !IsQualitySurface(path) {
			continue
		}
		if err := writeQualitySurfaceDigestEntry(hash, root, path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeQualitySurfaceDigestEntry(hash interface{ Write([]byte) (int, error) }, root, path string) error {
	_, _ = hash.Write([]byte(path))
	_, _ = hash.Write([]byte{0})
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		_, _ = hash.Write([]byte("missing\x00"))
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect quality surface %s: %w", path, err)
	}
	_, _ = hash.Write([]byte(info.Mode().String()))
	_, _ = hash.Write([]byte{0})
	if err := writeQualitySurfaceContent(hash, absolute, path, info.Mode()); err != nil {
		return err
	}
	_, _ = hash.Write([]byte{0})
	return nil
}

func writeQualitySurfaceContent(hash interface{ Write([]byte) (int, error) }, absolute, path string, mode os.FileMode) error {
	switch {
	case mode.IsRegular():
		content, err := os.ReadFile(absolute)
		if err != nil {
			return fmt.Errorf("read quality surface %s: %w", path, err)
		}
		_, _ = hash.Write(content)
	case mode&os.ModeSymlink != 0:
		target, err := os.Readlink(absolute)
		if err != nil {
			return fmt.Errorf("read quality surface symlink %s: %w", path, err)
		}
		_, _ = hash.Write([]byte(target))
	}
	return nil
}

func (w *Workflow) captureQualitySurfaceBaseline() error {
	digest, err := w.captureQualitySurface(w.config.RepoRoot)
	if err != nil {
		return err
	}
	if digest == "" {
		return w.state.Remove(qualitySurfaceBaselineStateKey)
	}
	return w.state.Write(qualitySurfaceBaselineStateKey, digest)
}

func (w *Workflow) verifyQualitySurfaceBaseline(phase string) (bool, error) {
	current, err := w.captureQualitySurface(w.config.RepoRoot)
	if err != nil {
		return true, w.failClosedQualitySurface(phase, "quality policy surfaceを再計測できません", err)
	}
	if current == "" {
		return false, nil
	}
	if !w.state.Exists(qualitySurfaceBaselineStateKey) {
		if err := w.state.Write(qualitySurfaceBaselineStateKey, current); err != nil {
			return true, w.failClosedQualitySurface(phase, "legacy taskのquality policy baselineを初期化できません", err)
		}
		return false, nil
	}
	baseline, err := w.state.Read(qualitySurfaceBaselineStateKey)
	if err != nil {
		return true, w.failClosedQualitySurface(phase, "worker開始時のquality policy baselineを読めません", err)
	}
	if strings.TrimSpace(baseline) == current {
		return false, nil
	}
	return true, w.failClosedQualitySurface(phase, "workerがquality policy surfaceを変更しました", nil)
}

func (w *Workflow) failClosedQualitySurface(phase, reason string, cause error) error {
	if err := w.state.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		return err
	}
	if cause != nil {
		reason = fmt.Sprintf("%s: %v", reason, cause)
	}
	return w.emitResult(qualitySurfaceFailClosedResult(phase, reason))
}

func qualitySurfaceFailClosedResult(phase, reason string) packet.Result {
	return packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             fmt.Sprintf("accepted quality policy surfaceの変更を検出したためreviewerを呼ばず停止しました(%s)", phase),
		RequirementCoverage: "通常worker taskからquality gate自体を変更できない機械境界で停止したため、task実装のacceptanceは未完了",
		Invariants:          "worker開始時のaccepted harnesslint/commentlint/configを保持し、worker自身がquality判定基準を弱体化できない",
		TestEvidence:        "worker開始時quality surface digestとworker結果受理前digestを比較",
		Issues:              reason,
		ResidualRisk:        "quality policy変更の意図とtask差分をSol/GPTが直接確認する必要がある",
		Targets:             []string{".golangci.yml, harnesslint/commentlint implementation and wrappers"},
		SolQuestion:         "quality policy変更を通常taskから除外し、accepted baselineへ戻した上でtaskを再開する",
	}
}

func qualityGateFixResult(report harnesslint.Report) packet.Result {
	issues := make([]string, 0, len(report.Violations))
	targetSet := make(map[string]struct{}, len(report.Violations))
	for _, violation := range report.Violations {
		issues = append(issues, fmt.Sprintf("%s %s:%d:%d %s", violation.Rule, violation.Path, violation.Line, violation.Column, violation.Message))
		if violation.Path != "" {
			targetSet[violation.Path] = struct{}{}
		}
	}
	targets := make([]string, 0, len(targetSet))
	for path := range targetSet {
		targets = append(targets, path)
	}
	sort.Strings(targets)
	return packet.Result{
		Status:              packet.StatusFixRequired,
		Risk:                packet.RiskHigh,
		Summary:             "machine quality gateがrepository-wide postconditionを拒否した",
		RequirementCoverage: "harnesslint --fix後のharnesslint checkをreviewer判断より前に機械検証した",
		Invariants:          "quality policy自体を変更せず、残ったnon-fixable violationはimplementation側を修正する",
		TestEvidence:        "harnesslint --fixおよびharnesslint checkのtyped violation結果",
		Issues:              strings.Join(issues, "\n"),
		ResidualRisk:        "修正後もmachine quality gate再実行と通常reviewが必要",
		Targets:             targets,
	}
}
