package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (b *evidenceExportBuilder) collectPredecessorAttempts(target EvidenceExportTarget, attempt AttemptRecord) error {
	runtimeTaskID := b.targetRuntimeTaskIdentity(target, attempt)
	if attempt.PredecessorAttemptID == "" && runtimeTaskID == "" {
		return nil
	}
	visited := map[string]bool{attempt.AttemptID: true}
	current := attempt
	for current.PredecessorAttemptID != "" {
		section, record, ok := b.resolvePredecessorAttempt(target, current, runtimeTaskID, visited)
		b.attemptSection.Predecessors = append(b.attemptSection.Predecessors, section)
		if !ok {
			break
		}
		current = record
	}
	if err := b.collectAssociatedPredecessorAttempts(target.Task.TaskPath, attempt.AttemptID, runtimeTaskID); err != nil {
		return err
	}
	return b.collectPredecessorValidationRuns(attempt)
}

func (b *evidenceExportBuilder) collectPredecessorValidationRuns(attempt AttemptRecord) error {
	module, err := b.predecessorValidationStore(attempt)
	if err != nil || module == nil {
		return err
	}
	for index := range b.attemptSection.Predecessors {
		section := &b.attemptSection.Predecessors[index]
		if section.Window == nil {
			continue
		}
		if err := b.collectPredecessorWindowRuns(module, section); err != nil {
			return err
		}
	}
	return nil
}

func (b *evidenceExportBuilder) predecessorValidationStore(attempt AttemptRecord) (*state.StateStore, error) {
	if b.validationStoreResolved {
		return b.validationStore, nil
	}
	b.validationStoreResolved = true
	if err := b.resolveValidationWorkspaceRoot(attempt); err != nil {
		return nil, err
	}
	if b.workspaceRoot == "" {
		return nil, nil
	}
	module, err := b.store.repositoryValidationStore(b.workspaceRoot)
	if err != nil || module == nil {
		return nil, err
	}
	b.validationStore = module
	return module, nil
}

func (b *evidenceExportBuilder) resolveValidationWorkspaceRoot(attempt AttemptRecord) error {
	workspace, resolved, err := b.store.attemptWorkspaceForExport(b.head, attempt)
	if err != nil {
		return fmt.Errorf("resolve attempt workspace for validation owner lookup: %w", err)
	}
	b.workspaceRootResolved = true
	if resolved {
		b.workspaceRoot = workspace.Root
	}
	return nil
}

func (b *evidenceExportBuilder) collectPredecessorWindowRuns(module *state.StateStore, section *EvidenceExportPredecessorSection) error {
	runs, err := os.ReadDir(module.Path(qualitygate.RunDirectory))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	for _, entry := range runs {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		if err := b.collectPredecessorGateRun(module, entry.Name(), section); err != nil {
			return err
		}
	}
	return nil
}

func (b *evidenceExportBuilder) collectPredecessorGateRun(module *state.StateStore, runID string, section *EvidenceExportPredecessorSection) error {
	record, err := qualitygate.Read(module, runID)
	if err != nil {
		b.entries = append(b.entries, EvidenceExportEntry{
			Path:       section.EntriesPrefix + "/state/" + qualitygate.RunDirectory + "/" + runID + "/" + qualitygate.RunFile,
			Unreadable: err.Error(), Basis: evidenceExportBasisRepositoryWorkspace,
		})
		return nil
	}
	if !validationRunBindsToWindow(record, section.RuntimeTaskID, b.workspaceRoot, section.Window.Start, section.Window.End) {
		return nil
	}
	prefix := section.EntriesPrefix + "/state/" + qualitygate.RunDirectory + "/" + runID
	binding := evidenceValidationBindingWorkspaceWindow
	if record.TaskID != "" {
		binding = evidenceValidationBindingTaskRecord
	}
	collected := collectedValidationRun{run: record, basis: evidenceExportBasisRepositoryWorkspace, binding: binding}
	b.addPredecessorFile(module.Path(qualitygate.RunRelativePath(runID)), prefix+"/"+qualitygate.RunFile)
	if b.addPredecessorFile(module.Path(filepath.Join(qualitygate.RunDirectory, runID, qualitygate.RunLog)), prefix+"/"+qualitygate.RunLog) {
		collected.logEntry = prefix + "/" + qualitygate.RunLog
	}
	b.validationRuns = append(b.validationRuns, collected)
	return nil
}

func (b *evidenceExportBuilder) addPredecessorFile(sourcePath, entryPath string) bool {
	if b.hasEntry(entryPath) {
		return true
	}
	unreadable := func(reason string) bool {
		b.entries = append(b.entries, EvidenceExportEntry{Path: entryPath, Unreadable: reason, Basis: evidenceExportBasisRepositoryWorkspace})
		return false
	}
	before, err := os.Lstat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			b.entries = append(b.entries, EvidenceExportEntry{Path: entryPath, Source: evidenceExportSourceGateRun, Missing: true, Basis: evidenceExportBasisRepositoryWorkspace})
			return false
		}
		return unreadable(err.Error())
	}
	if !before.Mode().IsRegular() {
		return unreadable("source is not a regular file")
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return unreadable(err.Error())
	}
	after, err := os.Lstat(sourcePath)
	if err != nil {
		return unreadable(err.Error())
	}
	entry := EvidenceExportEntry{
		Path: entryPath, Source: evidenceExportSourceGateRun,
		SHA256: digestBytes(data), Bytes: int64(len(data)),
		CollectedAt: b.observedAt, Basis: evidenceExportBasisRepositoryWorkspace,
		Changing: evidenceExportFileChanged(before, after) || after.ModTime().After(b.observedAt),
	}
	entry.Records, entry.TrailingFragment = evidenceExportJSONLStats(entryPath, data)
	b.commit(entry, data)
	return true
}

func (b *evidenceExportBuilder) collectAssociatedPredecessorAttempts(taskPath, targetAttemptID, runtimeTaskID string) error {
	if runtimeTaskID == "" {
		return nil
	}
	attempts, err := b.store.listAttempts()
	if err != nil {
		return fmt.Errorf("read attempt inventory for runtime task association sweep: %w", err)
	}
	for _, record := range attempts {
		b.sweepAssociatedPredecessor(record, targetAttemptID, taskPath, runtimeTaskID)
	}
	return nil
}

func (b *evidenceExportBuilder) sweepAssociatedPredecessor(record AttemptRecord, targetAttemptID, taskPath, runtimeTaskID string) {
	if record.AttemptID == targetAttemptID || record.SemanticTaskRef.TaskPath != taskPath ||
		record.AttemptSealID == "" {
		return
	}
	if section := b.predecessorSectionByID(record.AttemptID); section != nil &&
		(section.Status == evidenceExportPredecessorCollected || section.SealDigest != "") {
		return
	}
	associated, err := b.store.attemptRuntimeTaskIDs(b.head, record)
	if err != nil {
		b.discloseUnreadablePredecessor(record, fmt.Errorf("attempt %s canonical runtime association is unreadable: %w", record.AttemptID, err))
		return
	}
	if !containsString(associated, runtimeTaskID) {
		return
	}
	sealRef, seal, err := b.store.findAttemptSeal(b.head, record)
	if err != nil {
		b.discloseUnreadablePredecessor(record, fmt.Errorf("attempt %s seal is unreadable: %w", record.AttemptID, err))
		return
	}
	b.adoptAssociatedPredecessor(record, sealRef, seal, runtimeTaskID)
}

func (b *evidenceExportBuilder) discloseUnreadablePredecessor(record AttemptRecord, reason error) {
	if existing := b.predecessorSectionByID(record.AttemptID); existing != nil {
		if existing.Problem == "" {
			existing.Problem = reason.Error()
		}
		return
	}
	b.attemptSection.Predecessors = append(b.attemptSection.Predecessors, EvidenceExportPredecessorSection{
		AttemptID:     record.AttemptID,
		Task:          record.SemanticTaskRef,
		EntriesPrefix: evidenceExportPredecessorRoot + "/" + record.AttemptID,
		Relation:      evidenceExportPredecessorAssociationRelation,
		Status:        evidenceExportPredecessorPartial,
		Problem:       reason.Error(),
	})
}

func (b *evidenceExportBuilder) predecessorSectionByID(attemptID string) *EvidenceExportPredecessorSection {
	for index := range b.attemptSection.Predecessors {
		if b.attemptSection.Predecessors[index].AttemptID == attemptID {
			return &b.attemptSection.Predecessors[index]
		}
	}
	return nil
}

func (b *evidenceExportBuilder) adoptAssociatedPredecessor(record AttemptRecord, sealRef EvidenceObjectRef, seal AttemptSeal, runtimeTaskID string) {
	if existing := b.predecessorSectionByID(record.AttemptID); existing != nil {
		if b.verifyAndProjectPredecessor(existing, sealRef, seal, record, runtimeTaskID) {
			existing.Relation = evidenceExportPredecessorAssociationRelation
		}
		return
	}
	section := EvidenceExportPredecessorSection{
		AttemptID:     record.AttemptID,
		Task:          record.SemanticTaskRef,
		EntriesPrefix: evidenceExportPredecessorRoot + "/" + record.AttemptID,
		Relation:      evidenceExportPredecessorAssociationRelation,
		Status:        evidenceExportPredecessorPartial,
	}
	if b.verifyAndProjectPredecessor(&section, sealRef, seal, record, runtimeTaskID) {
		section.Status = evidenceExportPredecessorCollected
	}
	b.attemptSection.Predecessors = append(b.attemptSection.Predecessors, section)
}

func (b *evidenceExportBuilder) targetRuntimeTaskIdentity(target EvidenceExportTarget, attempt AttemptRecord) string {
	if target.RuntimeTaskID != "" {
		return target.RuntimeTaskID
	}
	if b.association != nil {
		return b.association.RuntimeTaskID
	}
	_, taskID, err := b.store.resolveExportRuntime(attempt)
	if err != nil {
		return ""
	}
	return taskID
}

func (b *evidenceExportBuilder) resolvePredecessorAttempt(target EvidenceExportTarget, current AttemptRecord, runtimeTaskID string, visited map[string]bool) (EvidenceExportPredecessorSection, AttemptRecord, bool) {
	predecessorID := current.PredecessorAttemptID
	section := EvidenceExportPredecessorSection{
		AttemptID:     predecessorID,
		EntriesPrefix: evidenceExportPredecessorRoot + "/" + predecessorID,
		Relation:      evidenceExportPredecessorRelation,
		Status:        evidenceExportPredecessorPartial,
	}
	if visited[predecessorID] {
		section.Problem = "predecessor chain contains a cycle"
		return section, AttemptRecord{}, false
	}
	visited[predecessorID] = true
	record, err := b.store.loadAttempt(predecessorID)
	if err != nil {
		section.Problem = "predecessor attempt record is unavailable"
		return section, AttemptRecord{}, false
	}
	section.Task = record.SemanticTaskRef
	if record.SemanticTaskRef.TaskPath != target.Task.TaskPath {
		section.Problem = "predecessor attempt binds a different task"
		return section, AttemptRecord{}, false
	}
	sealRef, seal, err := b.store.findAttemptSeal(b.head, record)
	if err != nil {
		section.Problem = "predecessor attempt seal is not published"
		return section, AttemptRecord{}, false
	}
	if current.ResumedFromSealID != sealRef.LogicalIdentity {
		section.Problem = "resumed-from seal identity does not match the predecessor attempt seal"
		return section, AttemptRecord{}, false
	}
	if !b.verifyAndProjectPredecessor(&section, sealRef, seal, record, runtimeTaskID) {
		return section, AttemptRecord{}, false
	}
	section.Status = evidenceExportPredecessorCollected
	return section, record, true
}

func (b *evidenceExportBuilder) verifyAndProjectPredecessor(section *EvidenceExportPredecessorSection, sealRef EvidenceObjectRef, seal AttemptSeal, record AttemptRecord, runtimeTaskID string) bool {
	association, err := predecessorSealAssociation(b.store, seal, record.AttemptID)
	if err != nil {
		section.Problem = err.Error()
		return false
	}
	if runtimeTaskID == "" {
		section.Problem = "target attempt runtime task association is unresolved"
		return false
	}
	if association == nil || association.RuntimeTaskID == "" {
		section.Problem = "predecessor runtime task association is missing"
		return false
	}
	if association.RuntimeTaskID != runtimeTaskID {
		section.Problem = "predecessor attempt binds a different runtime task"
		return false
	}
	return b.projectPredecessorSeal(section, sealRef, seal, association)
}

func (b *evidenceExportBuilder) projectPredecessorSeal(section *EvidenceExportPredecessorSection, sealRef EvidenceObjectRef, seal AttemptSeal, association *runtimeSessionAssociation) bool {
	section.SealDigest = sealRef.Digest
	section.Window = &EvidenceExportWindow{Start: seal.StartedAt, End: seal.SealedAt, EndBasis: evidenceExportBasisAttemptSeal}
	if association != nil {
		section.RuntimeTaskID = association.RuntimeTaskID
		section.SessionIDs = association.SessionIDs
	}
	b.addEvidenceObjectFile(sealRef, section.EntriesPrefix+"/seal.json", evidenceExportBasisPredecessorSeal)
	if err := b.materializeSealGroup(seal, section.EntriesPrefix, false); err != nil {
		section.Status = evidenceExportPredecessorPartial
		section.Problem = fmt.Sprintf("predecessor seal evidence is unreadable: %v", err)
		return false
	}
	return true
}

func predecessorSealAssociation(store *Store, seal AttemptSeal, attemptID string) (*runtimeSessionAssociation, error) {
	if len(seal.SessionAssociationRefs) == 0 {
		return nil, nil
	}
	var resolved *runtimeSessionAssociation
	for _, ref := range seal.SessionAssociationRefs {
		data, err := store.LoadEvidenceObject(ref)
		if err != nil {
			return nil, fmt.Errorf("predecessor session association is unreadable: %w", err)
		}
		var association runtimeSessionAssociation
		if err := json.Unmarshal(data, &association); err != nil {
			return nil, fmt.Errorf("predecessor session association is invalid: %w", err)
		}
		if association.AttemptID != attemptID {
			return nil, fmt.Errorf("predecessor session association binds attempt %s, not %s", association.AttemptID, attemptID)
		}
		if resolved != nil && resolved.RuntimeTaskID != association.RuntimeTaskID {
			return nil, fmt.Errorf("predecessor session associations disagree on the runtime task")
		}
		resolved = &association
	}
	return resolved, nil
}
