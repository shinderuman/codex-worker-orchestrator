package controller

import (
	"path/filepath"
)

const (
	evidenceExportInstructionPlanPath    = "IMPLEMENTATION_PLAN.local.md"
	evidenceExportInstructionRulesPath   = "IMPLEMENTATION_RULES.md"
	evidenceExportInstructionHistoryPath = "IMPLEMENTATION_HISTORY.md"

	evidenceExportSnapshotEntryDir = "snapshots"

	evidenceExportSourceInstructionSnapshot = "instruction-snapshot"

	evidenceExportBasisWorkspaceObservation = "workspace-at-observation"

	evidenceExportInstructionNotSealedReason = "instruction snapshot was not sealed at finalization; current workspace content is not attributed to a stopped attempt"
)

var evidenceExportInstructionFiles = []string{evidenceExportInstructionPlanPath, evidenceExportInstructionRulesPath, evidenceExportInstructionHistoryPath}

func (c *evidenceLiveCollector) collectInstructionSnapshots() {
	if c.prefix != evidenceExportRuntimeModeLive {
		c.discloseUnsealedInstructionSnapshots()
		return
	}
	for _, name := range evidenceExportInstructionFiles {
		c.addFileBasis(filepath.Join(c.workspace.Root, name),
			"live/"+evidenceExportSnapshotEntryDir+"/"+name,
			evidenceExportSourceInstructionSnapshot, evidenceExportBasisWorkspaceObservation, false)
	}
}

func (c *evidenceLiveCollector) discloseUnsealedInstructionSnapshots() {
	for _, name := range evidenceExportInstructionFiles {
		if c.builder.hasEntry("attempt/" + evidenceExportSnapshotEntryDir + "/" + name) {
			continue
		}
		c.recordUnreadable(c.prefix+"/"+evidenceExportSnapshotEntryDir+"/"+name, evidenceExportInstructionNotSealedReason)
	}
}
