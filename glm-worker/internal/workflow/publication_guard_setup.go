package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	PublicationGuardHookMissing       = "is missing"
	PublicationGuardHookNotRegular    = "is not a regular file"
	PublicationGuardHookEmpty         = "is empty"
	PublicationGuardHookNotExecutable = "is not executable"

	publicationTrackedHooksPath = ".githooks"
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
		if defect := publicationGuardHookDefect(resolved, name); defect != nil {
			report.Defects = append(report.Defects, *defect)
		}
	}
	report.Defects = publicationGuardPrimaryDefects(report.Defects)
	return report, nil
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

func VerifyPublicationGuardSetup(repoRoot string) error {
	report, err := InspectPublicationGuardSetup(repoRoot)
	if err != nil {
		return err
	}
	if len(report.Defects) == 0 {
		return nil
	}
	defects := make([]string, 0, len(report.Defects))
	for _, defect := range report.Defects {
		defects = append(defects, defect.Hook+" "+defect.Defect+" at "+defect.Path)
	}
	return fmt.Errorf("publication guard setup is not effective at %s: %s", report.HooksDir, strings.Join(defects, "; "))
}

func publicationGuardHookDefect(hooksDir, name string) *PublicationGuardHookDefect {
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
	return nil
}
