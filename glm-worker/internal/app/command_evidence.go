package app

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"

const evidenceUsage = "usage: glm-worker --evidence <manifest.json>"

func evidenceCommand(args []string) (Command, error) {
	if len(args) != 2 {
		return Command{}, machinecli.UsageErrorf("%s", evidenceUsage)
	}
	return Command{Mode: ModeEvidence, EvidenceManifest: args[1]}, nil
}
