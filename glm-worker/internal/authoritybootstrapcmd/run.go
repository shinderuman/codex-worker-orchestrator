package authoritybootstrapcmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type Output struct {
	AuthoritySnapshotSHA256 string `json:"authority_snapshot_sha256"`
	AuthorityKind           string `json:"authority_kind"`
	ActiveTask              string `json:"active_task"`
	ContentSHA256           string `json:"content_sha256"`
	ContentMatch            string `json:"content_match"`
	Content                 string `json:"content,omitempty"`
}

type snapshot struct {
	rules      []byte
	plan       []byte
	active     []byte
	activePath string
	hash       string
}

const (
	ContentMatchUnknown   = "unknown"
	ContentMatchUnchanged = "unchanged"
	ContentMatchChanged   = "changed"
)

const knownContentSHA256Flag = "--known-content-sha256"

const (
	rulesFile = "IMPLEMENTATION_RULES.md"
	planFile  = "IMPLEMENTATION_PLAN.local.md"
	usage     = "usage: glm-worker --authority <rules|plan|active> [--known-content-sha256 <hex>]"
)

func Build(args []string) (Output, error) {
	kind, knownContentSHA, err := parseArguments(args)
	if err != nil {
		return Output{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return Output{}, fmt.Errorf("authority bootstrap: get cwd: %w", err)
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		return Output{}, fmt.Errorf("authority bootstrap: %w", err)
	}
	return BuildFromRoot(root, kind, knownContentSHA)
}

func BuildFromRoot(root string, kind string, knownContentSHA string) (Output, error) {
	if !validKind(kind) {
		return Output{}, fmt.Errorf("%s", usage)
	}
	if knownContentSHA != "" && !validContentSHA256(knownContentSHA) {
		return Output{}, fmt.Errorf("%s", usage)
	}
	snap, err := loadSnapshot(root)
	if err != nil {
		return Output{}, fmt.Errorf("authority bootstrap: %w", err)
	}
	output, err := snapshotPart(kind, snap)
	if err != nil {
		return Output{}, fmt.Errorf("authority bootstrap: %w", err)
	}
	return applyKnownContent(output, knownContentSHA), nil
}

func parseArguments(args []string) (string, string, error) {
	if len(args) == 1 && validKind(args[0]) {
		return args[0], "", nil
	}
	if len(args) == 3 && validKind(args[0]) && args[1] == knownContentSHA256Flag && validContentSHA256(args[2]) {
		return args[0], strings.ToLower(args[2]), nil
	}
	return "", "", fmt.Errorf("%s", usage)
}

func validContentSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func applyKnownContent(output Output, knownContentSHA string) Output {
	if knownContentSHA == "" {
		output.ContentMatch = ContentMatchUnknown
		return output
	}
	if knownContentSHA == output.ContentSHA256 {
		output.ContentMatch = ContentMatchUnchanged
		output.Content = ""
		return output
	}
	output.ContentMatch = ContentMatchChanged
	return output
}

func validKind(kind string) bool {
	return kind == "rules" || kind == "plan" || kind == "active"
}

func findRepoRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	for {
		if regularFile(filepath.Join(current, rulesFile)) && regularFile(filepath.Join(current, planFile)) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("canonical authority files not found from %s", start)
		}
		current = parent
	}
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func loadSnapshot(root string) (snapshot, error) {
	rules, err := os.ReadFile(filepath.Join(root, rulesFile))
	if err != nil {
		return snapshot{}, fmt.Errorf("read %s: %w", rulesFile, err)
	}
	plan, err := os.ReadFile(filepath.Join(root, planFile))
	if err != nil {
		return snapshot{}, fmt.Errorf("read %s: %w", planFile, err)
	}
	activePath, err := taskcontract.ParsePlanSchedule(string(plan)).ActiveTask()
	if err != nil {
		return snapshot{}, err
	}
	activeFile := filepath.Join(root, filepath.FromSlash(activePath))
	info, err := os.Lstat(activeFile)
	if err != nil {
		return snapshot{}, fmt.Errorf("inspect ACTIVE task %s: %w", activePath, err)
	}
	if !info.Mode().IsRegular() {
		return snapshot{}, fmt.Errorf("ACTIVE task file %s is not a regular file (%s)", activePath, info.Mode().Type())
	}
	active, err := os.ReadFile(activeFile)
	if err != nil {
		return snapshot{}, fmt.Errorf("read ACTIVE task %s: %w", activePath, err)
	}
	return snapshot{
		rules:      rules,
		plan:       plan,
		active:     active,
		activePath: activePath,
		hash:       snapshotHash(rules, plan, activePath, active),
	}, nil
}

func snapshotHash(rules []byte, plan []byte, activePath string, active []byte) string {
	parts := [][]byte{rules, plan, []byte(activePath), active}
	joined := bytes.Join(parts, []byte{0})
	sum := sha256.Sum256(joined)
	return hex.EncodeToString(sum[:])
}

func snapshotPart(kind string, snap snapshot) (Output, error) {
	var content []byte
	switch kind {
	case "rules":
		content = snap.rules
	case "plan":
		content = snap.plan
	case "active":
		content = snap.active
	default:
		return Output{}, fmt.Errorf("unknown kind %q", kind)
	}
	if !utf8.Valid(content) {
		return Output{}, fmt.Errorf("%s authority content is not valid UTF-8", kind)
	}
	contentSum := sha256.Sum256(content)
	return Output{
		AuthoritySnapshotSHA256: snap.hash,
		AuthorityKind:           kind,
		ActiveTask:              snap.activePath,
		ContentSHA256:           hex.EncodeToString(contentSum[:]),
		ContentMatch:            ContentMatchUnknown,
		Content:                 string(content),
	}, nil
}
