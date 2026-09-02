package authoritybootstrapcmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	rulesFile = "IMPLEMENTATION_RULES.md"
	planFile  = "IMPLEMENTATION_PLAN.local.md"
	usage     = "usage: glm-parent-action authority <rules|plan|active>"
)

type snapshot struct {
	rules      []byte
	plan       []byte
	active     []byte
	activePath string
	hash       string
}

func Execute(args []string, stdout io.Writer) error {
	if len(args) != 1 || !validKind(args[0]) {
		return fmt.Errorf("%s", usage)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("authority bootstrap: get cwd: %w", err)
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		return fmt.Errorf("authority bootstrap: %w", err)
	}
	snap, err := loadSnapshot(root)
	if err != nil {
		return fmt.Errorf("authority bootstrap: %w", err)
	}
	if err := writeSnapshotPart(stdout, args[0], snap); err != nil {
		return fmt.Errorf("authority bootstrap: write output: %w", err)
	}
	return nil
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
	activePath, err := parseActivePath(plan)
	if err != nil {
		return snapshot{}, err
	}
	active, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(activePath)))
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

func parseActivePath(plan []byte) (string, error) {
	lines := strings.Split(string(plan), "\n")
	inside := false
	entries := make([]string, 0, 1)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "## ACTIVE" {
			if inside {
				return "", fmt.Errorf("duplicate ACTIVE section")
			}
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "- `") || !strings.HasSuffix(line, "`") {
			return "", fmt.Errorf("unexpected ACTIVE entry %q", line)
		}
		entries = append(entries, strings.TrimSuffix(strings.TrimPrefix(line, "- `"), "`"))
	}
	if !inside {
		return "", fmt.Errorf("ACTIVE section is missing")
	}
	if len(entries) != 1 {
		return "", fmt.Errorf("ACTIVE section must contain exactly one task, got %d", len(entries))
	}
	activePath := entries[0]
	if err := validateActivePath(activePath); err != nil {
		return "", err
	}
	return activePath, nil
}

func validateActivePath(activePath string) error {
	if !strings.HasPrefix(activePath, "IMPLEMENTATION_TASKS/") || !strings.HasSuffix(activePath, ".md") {
		return fmt.Errorf("invalid ACTIVE task path %q", activePath)
	}
	if filepath.IsAbs(activePath) || filepath.Clean(activePath) != filepath.FromSlash(activePath) {
		return fmt.Errorf("invalid ACTIVE task path %q", activePath)
	}
	return nil
}

func snapshotHash(rules []byte, plan []byte, activePath string, active []byte) string {
	parts := [][]byte{rules, plan, []byte(activePath), active}
	joined := bytes.Join(parts, []byte{0})
	sum := sha256.Sum256(joined)
	return hex.EncodeToString(sum[:])
}

func writeSnapshotPart(w io.Writer, kind string, snap snapshot) error {
	var content []byte
	switch kind {
	case "rules":
		content = snap.rules
	case "plan":
		content = snap.plan
	case "active":
		content = snap.active
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
	if _, err := fmt.Fprintf(w, "authority_snapshot_sha256=%s\nauthority_kind=%s\nactive_task=%s\n---\n", snap.hash, kind, snap.activePath); err != nil {
		return err
	}
	_, err := w.Write(content)
	return err
}
