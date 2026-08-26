package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	smokeEvidenceVersion = 1
	smokeEvidenceFile    = "install-smoke-evidence.jsonl"
	smokeLogDirectory    = "install-smoke-logs"

	SmokeResultPass = "pass"
	SmokeResultFail = "fail"

	SmokeClaudeProbeMissing   = "missing"
	SmokeClaudeProbeSupported = "supports-json-schema"
	SmokeClaudeProbeRejected  = "unsupported"
)

type SmokeEnvironment struct {
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	Platform  string `json:"platform"`
	ClaudeCLI string `json:"claude_cli"`
}

type SmokeIdentity struct {
	TreeDigest       string           `json:"tree_digest"`
	SmokeInputDigest string           `json:"smoke_input_digest"`
	Environment      SmokeEnvironment `json:"environment"`

	Head           string `json:"head,omitempty"`
	IndexDigest    string `json:"index_digest,omitempty"`
	WorktreeDigest string `json:"worktree_digest,omitempty"`
}

type SmokeEvidenceRecord struct {
	Version     int           `json:"version"`
	Result      string        `json:"result"`
	ExitCode    int           `json:"exit_code"`
	Role        string        `json:"role,omitempty"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at"`
	DurationMS  int64         `json:"duration_ms"`
	Identity    SmokeIdentity `json:"identity"`
}

type SmokeReuseDecision struct {
	Reusable bool
	Record   *SmokeEvidenceRecord
	Reason   string
}

func (a SmokeIdentity) Matches(other SmokeIdentity) bool {
	return a.MismatchAxis(other) == ""
}

func (a SmokeIdentity) MismatchAxis(other SmokeIdentity) string {
	var axes []string
	if a.TreeDigest != other.TreeDigest {
		axes = append(axes, "tree_digest")
	}
	if a.SmokeInputDigest != other.SmokeInputDigest {
		axes = append(axes, "smoke_input_digest")
	}
	if a.Environment != other.Environment {
		if a.Environment.GoVersion != other.Environment.GoVersion {
			axes = append(axes, "environment.go_version")
		}
		if a.Environment.GOOS != other.Environment.GOOS {
			axes = append(axes, "environment.goos")
		}
		if a.Environment.GOARCH != other.Environment.GOARCH {
			axes = append(axes, "environment.goarch")
		}
		if a.Environment.Platform != other.Environment.Platform {
			axes = append(axes, "environment.platform")
		}
		if a.Environment.ClaudeCLI != other.Environment.ClaudeCLI {
			axes = append(axes, "environment.claude_cli")
		}
	}
	return strings.Join(axes, ",")
}

func CaptureSmokeIdentity(repoRoot string, claudeBin string) (SmokeIdentity, error) {
	treeDigest, err := CaptureSmokeTreeDigest(repoRoot)
	if err != nil {
		return SmokeIdentity{}, err
	}
	inputDigest, err := CaptureSmokeInputDigest(repoRoot)
	if err != nil {
		return SmokeIdentity{}, err
	}
	environment, err := CaptureSmokeEnvironment(claudeBin)
	if err != nil {
		return SmokeIdentity{}, err
	}
	snapshot, err := CaptureGitSnapshot(repoRoot)
	if err != nil {
		return SmokeIdentity{}, err
	}
	return SmokeIdentity{
		TreeDigest:       treeDigest,
		SmokeInputDigest: inputDigest,
		Environment:      environment,
		Head:             snapshot.Head,
		IndexDigest:      snapshot.IndexDigest,
		WorktreeDigest:   snapshot.WorktreeDigestExcludingParent,
	}, nil
}

func CaptureSmokeTreeDigest(repoRoot string) (string, error) {
	paths, err := smokeDigestPaths(repoRoot, []string{"-z", "--cached"})
	if err != nil {
		return "", err
	}
	untracked, err := smokeDigestPaths(repoRoot, []string{"-z", "--others", "--exclude-standard"})
	if err != nil {
		return "", err
	}
	seen := make(map[string]bool, len(paths)+len(untracked))
	merged := make([]string, 0, len(paths)+len(untracked))
	for _, group := range [][]string{paths, untracked} {
		for _, path := range group {
			if !seen[path] {
				seen[path] = true
				merged = append(merged, path)
			}
		}
	}
	sort.Strings(merged)
	return hashSmokePaths(repoRoot, merged)
}

var smokeInputRoots = []string{
	"install.sh",
	"tests/install_smoke.sh",
	".githooks",
	"claude",
	"codex",
	"glm-worker",
	"tools",
}

func CaptureSmokeInputDigest(repoRoot string) (string, error) {
	merged := make([]string, 0)
	for _, root := range smokeInputRoots {
		absRoot := filepath.Join(repoRoot, filepath.FromSlash(root))
		err := filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			merged = append(merged, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("smoke input列挙 %s: %w", root, err)
		}
	}
	sort.Strings(merged)
	return hashSmokePaths(repoRoot, merged)
}

func smokeDigestPaths(repoRoot string, baseArgs []string) ([]string, error) {
	args := append([]string{"-C", repoRoot, "ls-files"}, baseArgs...)
	args = append(args, "--")
	args = append(args, ParentExcludePathspecs()...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	trimmed := strings.TrimRight(string(output), "\x00")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\x00"), nil
}

func hashSmokePaths(repoRoot string, paths []string) (string, error) {
	hasher := sha256.New()
	for _, path := range paths {
		absPath, err := joinWithinRoot(repoRoot, path)
		if err != nil {
			return "", fmt.Errorf("smoke identity %s: %w", path, err)
		}
		info, err := os.Lstat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				hasher.Write([]byte(path))
				hasher.Write([]byte("\x00absent\n"))
				continue
			}
			return "", fmt.Errorf("smoke identity file %sをstatできません: %w", path, err)
		}
		entryHash, err := hashSmokeEntry(absPath, info.Mode())
		if err != nil {
			return "", err
		}
		hasher.Write([]byte(path))
		hasher.Write([]byte{0})
		hasher.Write([]byte(entryHash))
		hasher.Write([]byte("\n"))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func hashSmokeEntry(absPath string, mode os.FileMode) (string, error) {
	switch {
	case mode.IsRegular():
		content, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("smoke identity file %sを読めません: %w", absPath, err)
		}
		sum := sha256.Sum256(content)
		execMarker := "noexec"
		if mode.Perm()&0o111 != 0 {
			execMarker = "exec"
		}
		return fmt.Sprintf("regular\x00%s\x00%s", execMarker, hex.EncodeToString(sum[:])), nil
	case mode&os.ModeSymlink != 0:
		target, err := os.Readlink(absPath)
		if err != nil {
			return "", fmt.Errorf("smoke identity symlink %sを読めません: %w", absPath, err)
		}
		sum := sha256.Sum256([]byte(target))
		return fmt.Sprintf("symlink\x00%s", hex.EncodeToString(sum[:])), nil
	default:
		return "", fmt.Errorf("smoke identity file %sは取り扱えないfile type %sです", absPath, mode.Type())
	}
}

func CaptureSmokeEnvironment(claudeBin string) (SmokeEnvironment, error) {
	goVersion, err := smokeExecOutput("go", "version")
	if err != nil {
		return SmokeEnvironment{}, err
	}
	goEnv, err := smokeExecOutput("go", "env", "GOOS", "GOARCH")
	if err != nil {
		return SmokeEnvironment{}, err
	}
	fields := strings.Fields(goEnv)
	if len(fields) != 2 {
		return SmokeEnvironment{}, fmt.Errorf("go env GOOS GOARCHの出力が期待と異なります: %q", goEnv)
	}
	platform, err := smokeExecOutput("uname", "-s", "-m")
	if err != nil {
		return SmokeEnvironment{}, err
	}
	environment := SmokeEnvironment{
		GoVersion: goVersion,
		GOOS:      fields[0],
		GOARCH:    fields[1],
		Platform:  platform,
		ClaudeCLI: probeSmokeClaudeCLI(claudeBin),
	}
	return environment, nil
}

func smokeExecOutput(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func probeSmokeClaudeCLI(claudeBin string) string {
	if _, err := exec.LookPath(claudeBin); err != nil {
		return SmokeClaudeProbeMissing
	}
	output, err := exec.Command(claudeBin, "--help").Output()
	if err != nil || !strings.Contains(string(output), "--json-schema") {
		return SmokeClaudeProbeRejected
	}
	return SmokeClaudeProbeSupported
}

func DecideSmokeReuse(records []SmokeEvidenceRecord, current SmokeIdentity) SmokeReuseDecision {
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		if !record.Identity.Matches(current) {
			continue
		}
		if record.Result == SmokeResultPass {
			return SmokeReuseDecision{Reusable: true, Record: &record, Reason: "identity-match"}
		}
		return SmokeReuseDecision{Reusable: false, Reason: "latest-matching-evidence-failed"}
	}
	if len(records) == 0 {
		return SmokeReuseDecision{Reusable: false, Reason: "no-evidence"}
	}
	latest := records[len(records)-1]
	return SmokeReuseDecision{Reusable: false, Reason: "stale:" + latest.Identity.MismatchAxis(current)}
}

func (s *StateStore) AppendSmokeEvidence(record SmokeEvidenceRecord) error {
	record.Version = smokeEvidenceVersion
	data, err := marshalSmokeEvidenceLine(record)
	if err != nil {
		return err
	}
	path := s.Path(smokeEvidenceFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (s *StateStore) ReadSmokeEvidence() ([]SmokeEvidenceRecord, error) {
	data, err := os.ReadFile(s.Path(smokeEvidenceFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []SmokeEvidenceRecord
	for index, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var record SmokeEvidenceRecord
		if err := unmarshalSmokeEvidenceLine(line, &record); err != nil {
			return nil, fmt.Errorf("install smoke evidence %d行目: %w", index+1, err)
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *StateStore) SmokeLogPath(treeDigest string, result string) string {
	prefix := treeDigest
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return s.Path(filepath.Join(smokeLogDirectory, prefix+"-"+result+".log"))
}

func marshalSmokeEvidenceLine(record SmokeEvidenceRecord) ([]byte, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("install smoke evidenceをJSON化できません: %w", err)
	}
	return data, nil
}

func unmarshalSmokeEvidenceLine(line string, record *SmokeEvidenceRecord) error {
	if err := json.Unmarshal([]byte(line), record); err != nil {
		return fmt.Errorf("install smoke evidenceを読めません: %w", err)
	}
	if record.Version != smokeEvidenceVersion {
		return fmt.Errorf("unsupported install smoke evidence version: %d", record.Version)
	}
	if record.Result != SmokeResultPass && record.Result != SmokeResultFail {
		return fmt.Errorf("install smoke evidence resultが不正です: %q", record.Result)
	}
	return nil
}
