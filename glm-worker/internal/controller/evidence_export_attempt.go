package controller

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

type evidenceExportGitArchiveMetadata struct {
	SchemaVersion   int                    `json:"schema_version"`
	LogicalIdentity string                 `json:"logical_identity"`
	CanonicalDigest string                 `json:"canonical_digest"`
	CanonicalBytes  int64                  `json:"canonical_bytes"`
	ObjectFormat    string                 `json:"object_format"`
	PackDigest      string                 `json:"pack_digest"`
	Roots           []GitObjectArchiveRoot `json:"roots"`
	OmittedPayload  string                 `json:"omitted_payload"`
}

type evidenceExportArchiveSource struct {
	ref     EvidenceObjectRef
	logical string
}

type evidenceExportTaskWalk struct {
	sealRef        EvidenceObjectRef
	seal           AttemptSeal
	foundSeal      bool
	finalizations  []EvidenceObjectRef
	lineageRecords []EvidenceObjectRef
	terminalRef    *EvidenceObjectRef
	references     []EvidenceExportReference
}

const evidenceKindRuntimeState = "runtime-state"

func (b *evidenceExportBuilder) collectAttemptSection(target EvidenceExportTarget, attempt AttemptRecord) (*EvidenceExportGitAudit, error) {
	b.attemptSection = EvidenceExportAttemptSection{Status: evidenceExportAttemptAbsent}
	walk, absent, err := b.walkAttemptEvidence(target, attempt)
	if err != nil {
		return nil, err
	}
	if absent != "" {
		b.attemptSection = EvidenceExportAttemptSection{Status: evidenceExportAttemptAbsent, AbsenceReason: absent}
		if err := b.collectPredecessorAttempts(target, attempt); err != nil {
			return nil, err
		}
		return nil, nil
	}
	b.attemptSection = EvidenceExportAttemptSection{
		Status:      evidenceExportAttemptPresent,
		SealDigest:  walk.sealRef.Digest,
		Disposition: walk.seal.Disposition,
		Coverage:    walk.seal.Coverage,
		Missing:     walk.seal.Missing,
		Unreadable:  walk.seal.Unreadable,
		Window: &EvidenceExportWindow{
			Start: walk.seal.StartedAt, End: walk.seal.SealedAt, EndBasis: evidenceExportBasisAttemptSeal,
		},
	}
	b.addEvidenceObjectFile(walk.sealRef, "attempt/seal.json", evidenceExportBasisAttemptSeal)
	if err := b.materializeSealEvidence(walk.seal); err != nil {
		return nil, err
	}
	if err := b.materializeAttemptRecords(target, walk); err != nil {
		return nil, err
	}
	if err := b.collectPredecessorAttempts(target, attempt); err != nil {
		return nil, err
	}
	if b.association != nil {
		b.attemptSection.RuntimeTaskID = b.association.RuntimeTaskID
		b.attemptSection.RuntimeEvidence = true
	}
	b.attemptSection.References = append(b.attemptSection.References, walk.references...)
	return b.buildAttemptGitAudit(attempt, walk.seal)
}

func (b *evidenceExportBuilder) walkAttemptEvidence(target EvidenceExportTarget, attempt AttemptRecord) (evidenceExportTaskWalk, string, error) {
	walk := evidenceExportTaskWalk{}
	if b.head.EvidenceHeadRef == nil {
		return walk, "controller evidence authority is not published yet", nil
	}
	evidenceHead, err := b.store.LoadEvidenceHead(*b.head.EvidenceHeadRef)
	if err != nil {
		return walk, "", err
	}
	subject, ok := evidenceSubjectHead(evidenceHead.TaskHeads, taskEvidenceSubjectID(target.Task))
	if !ok {
		return walk, "task evidence root is not published yet", nil
	}
	current := subject.RevisionRef
	for {
		revision, err := b.store.LoadTaskIndexRevision(current)
		if err != nil {
			return walk, "", err
		}
		walk.references = append(walk.references, evidenceExportReference(current, "task-index-revision", "task-revision-chain"))
		if err := b.collectRevisionRecords(revision, attempt, &walk); err != nil {
			return walk, "", err
		}
		if walk.foundSeal {
			break
		}
		if revision.PreviousRevision == nil {
			return walk, "attempt seal is not published for this attempt", nil
		}
		current = *revision.PreviousRevision
	}
	if b.head.EvidenceLedgerHeadRef != nil {
		walk.references = append(walk.references, evidenceExportReference(*b.head.EvidenceLedgerHeadRef, "evidence-ledger-record", "publication-ledger"))
	}
	walk.references = append(walk.references, evidenceExportReference(*b.head.EvidenceHeadRef, "evidence-head", "publication-head"))
	return walk, "", nil
}

func (b *evidenceExportBuilder) collectRevisionRecords(revision TaskIndexRevision, attempt AttemptRecord, walk *evidenceExportTaskWalk) error {
	if err := b.collectRevisionSeals(revision, attempt, walk); err != nil {
		return err
	}
	if err := b.collectRevisionFinalizations(revision, walk); err != nil {
		return err
	}
	collectRevisionLineage(revision, walk)
	if revision.TerminalRecord != nil {
		walk.terminalRef = revision.TerminalRecord
	}
	return nil
}

func (b *evidenceExportBuilder) collectRevisionSeals(revision TaskIndexRevision, attempt AttemptRecord, walk *evidenceExportTaskWalk) error {
	for _, ref := range revision.AttemptSeals {
		seal, err := b.store.LoadAttemptSeal(ref)
		if err != nil {
			return err
		}
		if seal.AttemptID == attempt.AttemptID && (attempt.AttemptSealID == "" || seal.AttemptSealID == attempt.AttemptSealID) {
			walk.sealRef = ref
			walk.seal = seal
			walk.foundSeal = true
			continue
		}
		walk.references = append(walk.references, evidenceExportReference(ref, "attempt-seal", "other-attempt-seal"))
	}
	return nil
}

func (b *evidenceExportBuilder) collectRevisionFinalizations(revision TaskIndexRevision, walk *evidenceExportTaskWalk) error {
	for _, ref := range revision.Finalizations {
		record, err := b.store.LoadAttemptFinalization(ref)
		if err != nil {
			return err
		}
		if walk.foundSeal && evidenceRefsEqual(record.AttemptSealRef, walk.sealRef) {
			walk.finalizations = append(walk.finalizations, ref)
			continue
		}
		walk.references = append(walk.references, evidenceExportReference(ref, "attempt-finalization", "other-attempt-finalization"))
	}
	return nil
}

func collectRevisionLineage(revision TaskIndexRevision, walk *evidenceExportTaskWalk) {
	for _, ref := range revision.PublicationLineageRecords {
		if ref.Kind == "accepted-candidate" {
			walk.lineageRecords = append(walk.lineageRecords, ref)
			continue
		}
		walk.references = append(walk.references, evidenceExportReference(ref, ref.Kind, "publication-lineage"))
	}
}

func (b *evidenceExportBuilder) materializeSealEvidence(seal AttemptSeal) error {
	return b.materializeSealGroup(seal, "attempt", true)
}

func (b *evidenceExportBuilder) materializeSealGroup(seal AttemptSeal, prefix string, targetAttempt bool) error {
	groups := append([]EvidenceObjectRef(nil), seal.EvidenceRefs...)
	groups = append(groups, seal.SessionAssociationRefs...)
	groups = append(groups, seal.ReviewValidationRefs...)
	groups = append(groups, seal.FindingRecordRefs...)
	seen := map[string]struct{}{}
	for _, ref := range groups {
		if _, duplicate := seen[ref.Digest]; duplicate {
			continue
		}
		seen[ref.Digest] = struct{}{}
		if err := b.materializeSealRef(seal, ref, prefix, targetAttempt); err != nil {
			return err
		}
	}
	return nil
}

func (b *evidenceExportBuilder) materializeSealRef(seal AttemptSeal, ref EvidenceObjectRef, prefix string, targetAttempt bool) error {
	basis := evidenceExportBasisAttemptSeal
	if !targetAttempt {
		basis = evidenceExportBasisPredecessorSeal
	}
	if ref.Kind == "session-association" {
		data, err := b.store.LoadEvidenceObject(ref)
		if err != nil {
			return err
		}
		var association runtimeSessionAssociation
		if err := json.Unmarshal(data, &association); err != nil {
			return fmt.Errorf("attempt seal session association is invalid: %w", err)
		}
		if targetAttempt {
			b.association = &association
		}
		b.addEvidenceObjectFile(ref, prefix+"/session-association.json", basis)
		return nil
	}
	entryPath, ok := evidenceExportAttemptEntryPath(seal.AttemptID, prefix, ref)
	if !ok {
		relation := "attempt-seal-unrouted"
		if !targetAttempt {
			relation = "predecessor-seal-unrouted"
		}
		b.attemptSection.References = append(b.attemptSection.References, evidenceExportReference(ref, ref.Kind, relation))
		return nil
	}
	if ref.Kind == evidenceKindCandidateEvidence {
		return b.materializeCandidateEvidence(ref, entryPath, basis)
	}
	b.addEvidenceObjectFile(ref, entryPath, basis)
	return nil
}

func (b *evidenceExportBuilder) materializeCandidateEvidence(ref EvidenceObjectRef, recordPath, basis string) error {
	data, err := b.store.LoadEvidenceObject(ref)
	if err != nil {
		return err
	}
	var record struct {
		Kind     string            `json:"kind"`
		Artifact EvidenceObjectRef `json:"artifact"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return fmt.Errorf("attempt seal candidate evidence is invalid: %w", err)
	}
	b.addEvidenceObjectFile(ref, recordPath, basis)
	if record.Artifact.Digest == "" {
		return nil
	}
	artifactPath, ok := evidenceExportAttemptArtifactPath(record.Kind)
	if !ok {
		artifactPath = "attempt/candidates/" + record.Kind + "-artifact"
	}
	b.addEvidenceObjectFile(record.Artifact, artifactPath, basis)
	return nil
}

func (b *evidenceExportBuilder) materializeAttemptRecords(target EvidenceExportTarget, walk evidenceExportTaskWalk) error {
	for _, ref := range walk.lineageRecords {
		if err := b.materializeLineageRecord(target, ref); err != nil {
			return err
		}
	}
	for index, ref := range walk.finalizations {
		b.addEvidenceObjectFile(ref, fmt.Sprintf("attempt/finalization/%d.json", index), evidenceExportBasisAttemptSeal)
	}
	if walk.terminalRef != nil {
		b.addEvidenceObjectFile(*walk.terminalRef, "attempt/terminal-task.json", evidenceExportBasisAttemptSeal)
		b.appendTerminalArchiveRef(*walk.terminalRef)
	}
	return nil
}

func (b *evidenceExportBuilder) materializeLineageRecord(target EvidenceExportTarget, ref EvidenceObjectRef) error {
	data, err := b.store.LoadEvidenceObject(ref)
	if err != nil {
		return err
	}
	var record struct {
		AttemptID  string              `json:"attempt_id"`
		GitArchive EvidenceObjectRef   `json:"git_archive"`
		Evidence   []EvidenceObjectRef `json:"evidence"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return fmt.Errorf("attempt lineage evidence is invalid: %w", err)
	}
	if record.AttemptID != "" && record.AttemptID != target.AttemptID {
		b.attemptSection.References = append(b.attemptSection.References, evidenceExportReference(ref, ref.Kind, "other-attempt-lineage"))
		return nil
	}
	b.addEvidenceObjectFile(ref, "attempt/accepted-candidate.json", evidenceExportBasisAttemptSeal)
	for _, evidence := range record.Evidence {
		if _, err := b.store.LoadEvidenceObject(evidence); err != nil {
			return err
		}
		b.attemptSection.References = append(b.attemptSection.References, evidenceExportReference(evidence, evidence.Kind, "accepted-candidate-evidence"))
	}
	if record.GitArchive.Digest != "" {
		b.archiveRefs = append(b.archiveRefs, evidenceExportArchiveSource{ref: record.GitArchive, logical: record.GitArchive.LogicalIdentity})
	}
	return nil
}

func (b *evidenceExportBuilder) appendTerminalArchiveRef(ref EvidenceObjectRef) {
	data, err := b.store.LoadEvidenceObject(ref)
	if err != nil {
		return
	}
	var record struct {
		GitArchive EvidenceObjectRef `json:"git_archive"`
	}
	if err := json.Unmarshal(data, &record); err != nil || record.GitArchive.Digest == "" {
		return
	}
	b.archiveRefs = append(b.archiveRefs, evidenceExportArchiveSource{ref: record.GitArchive, logical: record.GitArchive.LogicalIdentity})
}

func (b *evidenceExportBuilder) buildAttemptGitAudit(attempt AttemptRecord, seal AttemptSeal) (*EvidenceExportGitAudit, error) {
	audit := &EvidenceExportGitAudit{
		ExecutionBaseOID:     seal.ExecutionBaseOID,
		BaselineIndexTree:    seal.BaselineIndexTree,
		BaselineWorktreeTree: seal.BaselineWorktreeTree,
		CurrentIndexTree:     seal.CurrentIndexTree,
		CurrentWorktreeTree:  seal.CurrentWorktreeTree,
		CandidateCommitOID:   seal.CandidateCommitOID,
		CandidateTreeOID:     seal.CandidateTreeOID,
		CandidateBaseOID:     seal.CandidateBaseOID,
	}
	sources := []evidenceExportArchiveSource{
		{ref: attempt.BaselineArchive, logical: attempt.BaselineArchive.LogicalIdentity},
		{ref: seal.GitObjectArchive, logical: seal.GitObjectArchive.LogicalIdentity},
	}
	sources = append(sources, b.archiveRefs...)
	seen := map[string]struct{}{}
	for _, source := range sources {
		if source.ref.Digest == "" {
			continue
		}
		if _, duplicate := seen[source.ref.Digest]; duplicate {
			continue
		}
		seen[source.ref.Digest] = struct{}{}
		if err := b.appendGitArchiveMetadata(audit, source); err != nil {
			return nil, err
		}
	}
	b.collectAttemptTaskDiff(audit, seal)
	return audit, nil
}

func (b *evidenceExportBuilder) appendGitArchiveMetadata(audit *EvidenceExportGitAudit, source evidenceExportArchiveSource) error {
	metadata, err := b.store.gitArchiveMetadata(source.ref)
	if err != nil {
		b.recordUnreadable(fmt.Sprintf("attempt/git/archives/%d.json", len(audit.Archives)), err.Error())
		return nil
	}
	logical := source.logical
	if logical == "" {
		logical = source.ref.LogicalIdentity
	}
	audit.Archives = append(audit.Archives, EvidenceExportGitArchive{
		LogicalIdentity: logical,
		Digest:          source.ref.Digest,
		Bytes:           source.ref.Length,
		ObjectFormat:    metadata.ObjectFormat,
		PackDigest:      metadata.PackDigest,
		Roots:           metadata.Roots,
	})
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	entryPath := fmt.Sprintf("attempt/git/archives/%d.json", len(audit.Archives)-1)
	entry := EvidenceExportEntry{
		Path: entryPath, Source: "git-object-archive",
		SHA256: digestBytes(data), Bytes: int64(len(data)),
		CollectedAt: b.observedAt, Basis: evidenceExportBasisAttemptSeal,
		CanonicalDigest: source.ref.Digest, CanonicalKind: source.ref.Kind,
		LogicalIdentity: logical, OmittedPayload: evidenceExportPayloadOmission,
	}
	b.commit(entry, append(data, '\n'))
	return nil
}

func (s *Store) gitArchiveMetadata(ref EvidenceObjectRef) (evidenceExportGitArchiveMetadata, error) {
	if err := validateTypedEvidenceRef(ref, "git-object-archive"); err != nil {
		return evidenceExportGitArchiveMetadata{}, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return evidenceExportGitArchiveMetadata{}, err
	}
	var envelope gitObjectArchiveEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return evidenceExportGitArchiveMetadata{}, gitArchiveIntegrityError(ref, "git object archive envelope is invalid")
	}
	return evidenceExportGitArchiveMetadata{
		SchemaVersion:   envelope.SchemaVersion,
		LogicalIdentity: ref.LogicalIdentity,
		CanonicalDigest: ref.Digest,
		CanonicalBytes:  ref.Length,
		ObjectFormat:    envelope.ObjectFormat,
		PackDigest:      envelope.PackDigest,
		Roots:           envelope.Roots,
		OmittedPayload:  evidenceExportPayloadOmission,
	}, nil
}

func (b *evidenceExportBuilder) collectAttemptTaskDiff(audit *EvidenceExportGitAudit, seal AttemptSeal) {
	var args []string
	var basis string
	switch {
	case seal.CandidateCommitOID != "" && seal.ExecutionBaseOID != "" && seal.CandidateCommitOID != seal.ExecutionBaseOID:
		args = []string{seal.ExecutionBaseOID, seal.CandidateCommitOID}
		basis = "execution-base..candidate-commit"
	case seal.BaselineWorktreeTree != "" && seal.CurrentWorktreeTree != "" && seal.BaselineWorktreeTree != seal.CurrentWorktreeTree:
		args = []string{seal.BaselineWorktreeTree, seal.CurrentWorktreeTree}
		basis = "baseline-worktree-tree..current-worktree-tree"
	default:
		return
	}
	diff, err := runGitBinary(b.store.config.RepoRoot, nil, "diff", "--binary", args[0], args[1])
	entryPath := "attempt/git/task-diff.patch"
	if err != nil {
		b.recordUnreadable(entryPath, err.Error())
		return
	}
	entry := EvidenceExportEntry{
		Path: entryPath, Source: "git-diff",
		SHA256: digestBytes(diff), Bytes: int64(len(diff)),
		CollectedAt: b.observedAt, Basis: basis,
	}
	b.commit(entry, diff)
	audit.TaskDiff = &EvidenceExportGitTaskDiff{Basis: basis, Path: entryPath}
}

func evidenceExportAttemptEntryPath(attemptID, prefix string, ref EvidenceObjectRef) (string, bool) {
	if !evidenceExportAttemptKindRoutable(ref.Kind) {
		return "", false
	}
	suffix := strings.TrimPrefix(ref.LogicalIdentity, attemptID+":")
	if suffix == ref.LogicalIdentity || suffix == "" {
		return "", false
	}
	if ref.Kind == evidenceKindCandidateEvidence {
		if !evidenceExportSegmentSafe(suffix) {
			return "", false
		}
		return prefix + "/candidates/" + suffix + ".json", true
	}
	if transcript, ok := evidenceExportTranscriptEntrySuffix(ref.Kind, suffix); ok {
		suffix = transcript
	}
	slash := path.Clean(filepath.ToSlash(suffix))
	if slash == "." || slash == ".." || strings.HasPrefix(slash, "../") || strings.HasPrefix(slash, "/") || strings.Contains(slash, `\`) {
		return "", false
	}
	return prefix + "/" + slash, true
}

func evidenceExportTranscriptEntrySuffix(kind, suffix string) (string, bool) {
	var sourcePrefix, target string
	switch kind {
	case evidenceKindModelTranscript:
		sourcePrefix, target = "session/", "transcripts/model/"
	case evidenceKindParentTranscript:
		sourcePrefix, target = "parent/", "transcripts/parent/"
	case evidenceKindGuardianTranscript:
		sourcePrefix, target = "guardian/", "transcripts/guardian/"
	default:
		return "", false
	}
	rest := strings.TrimPrefix(suffix, sourcePrefix)
	if rest == suffix || rest == "" {
		return "", false
	}
	return target + rest, true
}

func evidenceExportAttemptKindRoutable(kind string) bool {
	switch kind {
	case evidenceKindRuntimeState, "telemetry", "task-events", "review-rounds", "task-lifecycle",
		"task-authority", "runtime-artifact", evidenceKindModelTranscript, evidenceKindParentTranscript,
		evidenceKindGuardianTranscript, evidenceKindCandidateEvidence, "finding-record", "instruction-snapshot":
		return true
	default:
		return false
	}
}

func evidenceExportAttemptArtifactPath(kind string) (string, bool) {
	switch kind {
	case candidateEvidenceKindReview, candidateEvidenceKindValidation, candidateEvidenceKindInstall:
		return "attempt/candidates/" + kind + "-artifact.json", true
	default:
		return "", false
	}
}

func (b *evidenceExportBuilder) addEvidenceObjectFile(ref EvidenceObjectRef, entryPath, basis string) {
	if ref.Kind == evidenceKindRuntimeState && evidenceExportEphemeralStateFile(path.Base(entryPath)) {
		return
	}
	data, err := b.store.LoadEvidenceObject(ref)
	if err != nil {
		b.recordUnreadable(entryPath, err.Error())
		return
	}
	if ref.Kind == evidenceKindRuntimeState && path.Base(entryPath) == evidenceExportParentEvidenceFile {
		b.commitParentEvidenceAggregate(ref, data, entryPath)
		return
	}
	entry := EvidenceExportEntry{
		Path: entryPath, Source: ref.Kind,
		SHA256: digestBytes(data), Bytes: int64(len(data)),
		CollectedAt:     b.observedAt,
		Basis:           basis,
		CanonicalDigest: ref.Digest, CanonicalKind: ref.Kind, LogicalIdentity: ref.LogicalIdentity,
	}
	entry.Records, entry.TrailingFragment = evidenceExportJSONLStats(entryPath, data)
	b.commit(entry, data)
}

func (b *evidenceExportBuilder) recordUnreadable(entryPath, reason string) {
	b.entries = append(b.entries, EvidenceExportEntry{Path: entryPath, Unreadable: reason})
}

func (b *evidenceExportBuilder) commitParentEvidenceAggregate(ref EvidenceObjectRef, data []byte, rawEntryPath string) {
	aggregatePath := strings.TrimSuffix(rawEntryPath, ".jsonl") + ".aggregate.json"
	aggregate, err := aggregateParentEvidence(data, rawEntryPath)
	if err != nil {
		b.recordUnreadable(aggregatePath, err.Error())
		return
	}
	b.commit(EvidenceExportEntry{
		Path: aggregatePath, Source: ref.Kind,
		SHA256: digestBytes(aggregate), Bytes: int64(len(aggregate)),
		CollectedAt:     b.observedAt,
		Basis:           evidenceExportBasisAttemptSeal,
		CanonicalDigest: ref.Digest, CanonicalKind: ref.Kind, LogicalIdentity: ref.LogicalIdentity,
		OmittedPayload: evidenceExportParentEvidenceOmission,
	}, aggregate)
}

func (b *evidenceExportBuilder) commit(entry EvidenceExportEntry, data []byte) {
	b.files = append(b.files, EvidenceExportFile{Path: entry.Path, Data: data})
	b.entries = append(b.entries, entry)
}

func evidenceExportReference(ref EvidenceObjectRef, kind, relation string) EvidenceExportReference {
	return EvidenceExportReference{Kind: kind, Digest: ref.Digest, LogicalIdentity: ref.LogicalIdentity, Relation: relation}
}
