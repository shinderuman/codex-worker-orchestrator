package failurepathtrial

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type TriggerDecision struct {
	Triggered        bool
	Classes          []string
	AmbiguousClasses []string
	TriggerPaths     []string
}

type triggerClassSpec struct {
	class  string
	paths  []string
	tokens []string
}

const (
	ClassExternalModelInvocation   = "external-model-invocation"
	ClassMetricReductionAccounting = "metric-reduction-accounting"
	ClassExternalOutputPersistence = "external-output-persistence"
	ClassReviewLifecycleRouting    = "review-lifecycle-routing"
)

const emptyTreeObject = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

const diffReadBoundBytes = 512 * 1024

var Classes = []string{
	ClassExternalModelInvocation,
	ClassMetricReductionAccounting,
	ClassExternalOutputPersistence,
	ClassReviewLifecycleRouting,
}

var triggerClassSpecs = []triggerClassSpec{
	{
		class: ClassExternalModelInvocation,
		paths: []string{"glm-worker/internal/runner/"},
		tokens: []string{
			"newProcessGroupCmd",
			"ClaudeBin",
			"exec.Command",
			"runProbeCommand",
			"ProbeWithDeadline",
			"--model",
			"--effort",
			"ANTHROPIC_BASE_URL",
		},
	},
	{
		class: ClassMetricReductionAccounting,
		paths: []string{
			"glm-worker/internal/abeval/",
			"glm-worker/internal/shadoweval/",
			"glm-worker/internal/state/stats.go",
			"glm-worker/internal/state/telemetry.go",
			"glm-worker/internal/state/callstats.go",
			"glm-worker/internal/state/routingeval.go",
		},
		tokens: []string{
			"Tokens",
			"Usage",
			"reduction",
			"Reduction",
			"CostUSD",
			"total_cost",
			"AgreementCount",
			"Brier",
		},
	},
	{
		class: ClassExternalOutputPersistence,
		paths: []string{
			"glm-worker/internal/report/",
			"glm-worker/internal/app/bundle.go",
			"glm-worker/internal/app/shadow_eval.go",
		},
		tokens: []string{
			"WriteFile",
			"MkdirAll",
			"ArtifactDir",
			"os.Create",
			"artifact",
		},
	},
	{
		class: ClassReviewLifecycleRouting,
		paths: []string{
			"glm-worker/internal/workflow/",
			"glm-worker/internal/state/resume",
			"glm-worker/internal/state/parent_review",
			"glm-worker/internal/state/new_task_transition.go",
		},
		tokens: []string{
			"reviewUntilStable",
			"handleReviewResult",
			"ResumeStage",
			"ResumeCheckpoint",
			"reviewerModel",
			"TaskStatus",
			"EnterStop",
			"RiskFloor",
			"reviewer-",
		},
	},
}

func ClassifyTrigger(repoRoot, baselineHead string, changedPaths []string) (TriggerDecision, error) {
	decision := TriggerDecision{}
	base := strings.TrimSpace(baselineHead)
	if base == "" {
		base = emptyTreeObject
	}
	for _, spec := range triggerClassSpecs {
		candidates := classCandidatePaths(spec, changedPaths)
		if len(candidates) == 0 {
			continue
		}
		text, err := changedPathDiffText(repoRoot, base, candidates)
		if err != nil {
			return TriggerDecision{}, err
		}
		if diffConfirmsClass(text, spec.tokens) {
			decision.Classes = append(decision.Classes, spec.class)
			decision.TriggerPaths = appendUniquePaths(decision.TriggerPaths, candidates...)
			continue
		}
		decision.AmbiguousClasses = append(decision.AmbiguousClasses, spec.class)
	}
	decision.Triggered = len(decision.Classes) > 0
	return decision, nil
}

func classCandidatePaths(spec triggerClassSpec, changedPaths []string) []string {
	var candidates []string
	for _, path := range changedPaths {
		if !triggerCandidatePath(path) {
			continue
		}
		for _, specPath := range spec.paths {
			if specPathMatches(specPath, path) {
				candidates = append(candidates, path)
				break
			}
		}
	}
	return candidates
}

func triggerCandidatePath(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasSuffix(path, "_test.go") {
		return false
	}
	return !strings.Contains(path, "testdata/")
}

func specPathMatches(specPath, path string) bool {
	if strings.HasSuffix(specPath, "/") {
		return strings.HasPrefix(path, specPath)
	}
	return path == specPath
}

func diffConfirmsClass(text string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func changedPathDiffText(repoRoot, base string, paths []string) (string, error) {
	tracked, err := gitOutput(repoRoot, append([]string{"diff", "--no-renames", base, "--"}, paths...)...)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString(boundText(tracked, diffReadBoundBytes))
	for _, path := range paths {
		if builder.Len() >= diffReadBoundBytes {
			break
		}
		content, included, err := untrackedFileContent(repoRoot, path)
		if err != nil {
			return "", err
		}
		if !included {
			continue
		}
		builder.WriteString(boundText(content, diffReadBoundBytes-builder.Len()))
	}
	return builder.String(), nil
}

func untrackedFileContent(repoRoot, path string) (string, bool, error) {
	tracked, err := gitOutput(repoRoot, "ls-files", "--", path)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(tracked) != "" {
		return "", false, nil
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failure-path trial対象file %sを読めません: %w", path, err)
	}
	return string(data), true, nil
}

func gitOutput(repoRoot string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("failure-path trialのgit %sが失敗しました: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(output), nil
}

func appendUniquePaths(paths []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(paths)+len(additions))
	for _, path := range paths {
		seen[path] = struct{}{}
	}
	for _, path := range additions {
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}
