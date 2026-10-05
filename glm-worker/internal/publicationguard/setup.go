package publicationguard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type PublicationGuardHookDefect struct {
	Hook   string
	Path   string
	Defect string
}

type PublicationGuardSetupReport struct {
	HooksPath        string
	HooksDir         string
	TrackedHooksMode bool
	Defects          []PublicationGuardHookDefect
}

const (
	Contract = "controller-publication-guard-v1"

	PublicationGuardHookMissing          = "is missing"
	PublicationGuardHookNotRegular       = "is not a regular file"
	PublicationGuardHookEmpty            = "is empty"
	PublicationGuardHookNotExecutable    = "is not executable"
	PublicationGuardHookIdentityMismatch = "does not match the committed canonical hook"
	PublicationGuardBindingInvalid       = "does not name an absolute executable regular file"
	PublicationGuardBindingContract      = "does not expose the canonical publication guard contract"

	publicationTrackedHooksPath  = ".githooks"
	publicationGuardBindingName  = "glm-publication-guard.path"
	publicationGuardProbeTimeout = 5 * time.Second
)

var publicationGuardHookNames = []string{"reference-transaction", "pre-push"}

func InspectPublicationGuardSetup(repoRoot string) (PublicationGuardSetupReport, error) {
	report := PublicationGuardSetupReport{}
	output, err := exec.Command("git", "-C", repoRoot, "config", "--get", "core.hooksPath").Output()
	if err != nil {
		return report, fmt.Errorf("publication guard setup cannot read core.hooksPath: %w", err)
	}
	report.HooksPath = strings.TrimSpace(string(output))
	if report.HooksPath == "" {
		return report, fmt.Errorf("publication guard setup is not demonstrably effective: core.hooksPath is not configured")
	}
	report.TrackedHooksMode = report.HooksPath == publicationTrackedHooksPath
	hooksDir := report.HooksPath
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(repoRoot, hooksDir)
	}
	resolved, err := filepath.EvalSymlinks(hooksDir)
	if err != nil {
		return report, fmt.Errorf("publication guard setup hook directory %s is unavailable: %w", hooksDir, err)
	}
	report.HooksDir = resolved
	for _, name := range publicationGuardHookNames {
		if defect := publicationGuardHookDefect(repoRoot, resolved, name); defect != nil {
			report.Defects = append(report.Defects, *defect)
		}
	}
	if defect := publicationGuardBindingDefect(resolved); defect != nil {
		report.Defects = append(report.Defects, *defect)
	}
	report.Defects = publicationGuardPrimaryDefects(report.Defects)
	return report, nil
}

func publicationGuardBindingDefect(hooksDir string) *PublicationGuardHookDefect {
	path := filepath.Join(hooksDir, publicationGuardBindingName)
	target, defect := publicationGuardBindingTarget(path)
	if defect != nil {
		return defect
	}
	if !publicationGuardHasCanonicalContract(target) {
		return &PublicationGuardHookDefect{Hook: publicationGuardBindingName, Path: path, Defect: PublicationGuardBindingContract}
	}
	return nil
}

func publicationGuardBindingTarget(path string) (string, *PublicationGuardHookDefect) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", &PublicationGuardHookDefect{Hook: publicationGuardBindingName, Path: path, Defect: PublicationGuardHookMissing}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", &PublicationGuardHookDefect{Hook: publicationGuardBindingName, Path: path, Defect: PublicationGuardHookNotRegular}
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return "", &PublicationGuardHookDefect{Hook: publicationGuardBindingName, Path: path, Defect: PublicationGuardHookEmpty}
	}
	lineEnd := bytes.IndexByte(data, '\n')
	if lineEnd <= 0 {
		return "", &PublicationGuardHookDefect{Hook: publicationGuardBindingName, Path: path, Defect: PublicationGuardBindingInvalid}
	}
	target := string(data[:lineEnd])
	if !filepath.IsAbs(target) {
		return "", &PublicationGuardHookDefect{Hook: publicationGuardBindingName, Path: path, Defect: PublicationGuardBindingInvalid}
	}
	targetInfo, err := os.Stat(target)
	if err != nil || !targetInfo.Mode().IsRegular() || targetInfo.Mode().Perm()&0o111 == 0 {
		return "", &PublicationGuardHookDefect{Hook: publicationGuardBindingName, Path: path, Defect: PublicationGuardBindingInvalid}
	}
	return target, nil
}

func publicationGuardHasCanonicalContract(target string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), publicationGuardProbeTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, target, "probe").Output()
	return err == nil && string(output) == Contract+"\n"
}

func publicationGuardPrimaryDefects(defects []PublicationGuardHookDefect) []PublicationGuardHookDefect {
	hasNonRepairable := false
	for _, defect := range defects {
		if defect.Defect != PublicationGuardHookNotExecutable {
			hasNonRepairable = true
			break
		}
	}
	if !hasNonRepairable {
		return defects
	}
	primary := make([]PublicationGuardHookDefect, 0, len(defects))
	for _, defect := range defects {
		if defect.Defect != PublicationGuardHookNotExecutable {
			primary = append(primary, defect)
		}
	}
	return primary
}

func publicationGuardHookDefect(repoRoot, hooksDir, name string) *PublicationGuardHookDefect {
	path := filepath.Join(hooksDir, name)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return &PublicationGuardHookDefect{Hook: name, Path: path, Defect: PublicationGuardHookMissing}
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return &PublicationGuardHookDefect{Hook: name, Path: path, Defect: PublicationGuardHookNotRegular}
	}
	if info.Size() == 0 {
		return &PublicationGuardHookDefect{Hook: name, Path: path, Defect: PublicationGuardHookEmpty}
	}
	if info.Mode().Perm()&0o111 == 0 {
		return &PublicationGuardHookDefect{Hook: name, Path: path, Defect: PublicationGuardHookNotExecutable}
	}
	active, err := os.ReadFile(resolved)
	if err != nil {
		return &PublicationGuardHookDefect{Hook: name, Path: path, Defect: PublicationGuardHookIdentityMismatch}
	}
	canonical, err := exec.Command("git", "-C", repoRoot, "show", "HEAD:.githooks/"+name).Output()
	if err != nil || !bytes.Equal(active, canonical) {
		return &PublicationGuardHookDefect{Hook: name, Path: path, Defect: PublicationGuardHookIdentityMismatch}
	}
	return nil
}
