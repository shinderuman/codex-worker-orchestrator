package authoritybootstrapcmd

import (
	"fmt"
	"os"
)

type BootstrapPart struct {
	ContentSHA256 string `json:"content_sha256"`
	Content       string `json:"content"`
}

type BootstrapOutput struct {
	AuthoritySnapshotSHA256 string        `json:"authority_snapshot_sha256"`
	ActiveTask              string        `json:"active_task"`
	Rules                   BootstrapPart `json:"rules"`
	Plan                    BootstrapPart `json:"plan"`
	Active                  BootstrapPart `json:"active"`
}

func BuildCommand(args []string) (any, error) {
	if len(args) == 1 && args[0] == "bootstrap" {
		return BuildBootstrap()
	}
	return Build(args)
}

func BuildBootstrap() (BootstrapOutput, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return BootstrapOutput{}, fmt.Errorf("authority bootstrap: get cwd: %w", err)
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		return BootstrapOutput{}, fmt.Errorf("authority bootstrap: %w", err)
	}
	return buildBootstrapFromRoot(root)
}

func buildBootstrapFromRoot(root string) (BootstrapOutput, error) {
	snap, err := loadSnapshot(root)
	if err != nil {
		return BootstrapOutput{}, fmt.Errorf("authority bootstrap: %w", err)
	}
	return projectBootstrap(snap)
}

func projectBootstrap(snap snapshot) (BootstrapOutput, error) {
	rules, err := snapshotPart("rules", snap)
	if err != nil {
		return BootstrapOutput{}, err
	}
	plan, err := snapshotPart("plan", snap)
	if err != nil {
		return BootstrapOutput{}, err
	}
	active, err := snapshotPart("active", snap)
	if err != nil {
		return BootstrapOutput{}, err
	}
	return BootstrapOutput{
		AuthoritySnapshotSHA256: snap.hash,
		ActiveTask:              snap.activePath,
		Rules: BootstrapPart{
			ContentSHA256: rules.ContentSHA256,
			Content:       rules.Content,
		},
		Plan: BootstrapPart{
			ContentSHA256: plan.ContentSHA256,
			Content:       plan.Content,
		},
		Active: BootstrapPart{
			ContentSHA256: active.ContentSHA256,
			Content:       active.Content,
		},
	}, nil
}
