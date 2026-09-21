package authoritybootstrapcmd

import (
	"fmt"
	"os"
)

// BootstrapPartはBootstrapOutputの単一snapshotへbindされたcanonical authority本文を表す。
// kindはcontainer fieldで表現するため、partごとのsnapshot metadataは重複させない。
type BootstrapPart struct {
	ContentSHA256 string `json:"content_sha256"`
	Content       string `json:"content"`
}

// BootstrapOutputはrules / plan / ACTIVE taskを1つのrepository snapshotから投影する。
// parent bootstrap/resumeは3個の独立callから整合性を再構成せず、このobjectをatomicに消費する。
type BootstrapOutput struct {
	AuthoritySnapshotSHA256 string        `json:"authority_snapshot_sha256"`
	ActiveTask              string        `json:"active_task"`
	Rules                   BootstrapPart `json:"rules"`
	Plan                    BootstrapPart `json:"plan"`
	Active                  BootstrapPart `json:"active"`
}

// BuildCommandはCLI authority projectionの入口である。既存per-kind projectionはbounded evidence read用に維持し、
// bootstrapをparentのcanonical fresh/resume surfaceとして扱う。
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
