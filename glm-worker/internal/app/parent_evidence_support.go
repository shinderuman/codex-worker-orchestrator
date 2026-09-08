package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type DuplicateParentProjectionError struct {
	Surface     string
	Digest      string
	OwnerCallID string
}

type parentReadDecision int

const parentEvidenceBatchCommand = "glm-parent-action evidence <manifest.json>"

const parentEvidenceUnchangedReason = "identical projection was already delivered within this decision lease"

const parentEvidenceLedgerLockFile = "parent-evidence-ledger.lock"

const (
	parentReadServe parentReadDecision = iota
	parentReadDuplicate
)

func (e *DuplicateParentProjectionError) Error() string {
	return fmt.Sprintf(
		"parent projection for surface %q was already delivered by evidence owner call %s; use %s instead of repeating the fine-grained read",
		e.Surface, e.OwnerCallID, parentEvidenceBatchCommand,
	)
}

func parentEvidenceLeaseActive(st *state.StateStore) bool {
	switch st.TaskStatus() {
	case state.TaskStatusWaitingDecision, state.TaskStatusWaitingSolReview, state.TaskStatusParked:
		return true
	default:
		return false
	}
}

func parentEvidenceDigest(value any) (string, int) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		sum := sha256.Sum256([]byte(err.Error()))
		return hex.EncodeToString(sum[:]), 0
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), len(data)
}

func parentEvidenceStringDigest(parts ...string) string {
	hasher := sha256.New()
	for _, part := range parts {
		hasher.Write([]byte(part))
		hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func parentEvidenceStorePresent(st *state.StateStore) bool {
	return st != nil && st.Present()
}

func recordParentEvidence(st *state.StateStore, record state.ParentEvidenceRecord) {
	if !parentEvidenceStorePresent(st) {
		return
	}
	if record.TaskID == "" {
		record.TaskID = st.ReadOr("task.id", "")
	}
	st.RecordParentEvidence(record)
}

func decideParentRead(st *state.StateStore, surface, digest string) (parentReadDecision, state.ParentEvidenceLedgerEntry, error) {
	if !parentEvidenceStorePresent(st) || !parentEvidenceLeaseActive(st) {
		return parentReadServe, state.ParentEvidenceLedgerEntry{}, nil
	}
	entry, delivered, err := st.ParentEvidenceDelivered(surface, digest)
	if err != nil {
		return parentReadServe, entry, err
	}
	if !delivered {
		return parentReadServe, state.ParentEvidenceLedgerEntry{}, nil
	}
	return parentReadDuplicate, entry, nil
}

func withParentEvidenceLedgerLock(st *state.StateStore, body func() error) error {
	if !parentEvidenceStorePresent(st) || !parentEvidenceLeaseActive(st) {
		return body()
	}
	lock, err := repolock.AcquireWait(st.Path(parentEvidenceLedgerLockFile))
	if err != nil {
		return fmt.Errorf("parent evidence ledger lockを取得できません: %w", err)
	}
	defer func() { _ = lock.Close() }()
	return body()
}

func writeMeasuredJSON(stdout io.Writer, value any) (int, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return 0, err
	}
	data := buf.Bytes()
	if _, err := stdout.Write(data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func saveParentEvidenceLedger(st *state.StateStore, surface, digest, origin, ownerCallID string) error {
	if !parentEvidenceStorePresent(st) || !parentEvidenceLeaseActive(st) {
		return nil
	}
	if err := st.SaveParentEvidenceLedgerEntry(state.ParentEvidenceLedgerEntry{
		Surface: surface, Digest: digest, Origin: origin, OwnerCallID: ownerCallID,
	}); err != nil {
		return fmt.Errorf("parent evidence ledgerを保存できません: %w", err)
	}
	return nil
}

func finishParentRead(st *state.StateStore, surface, digest string, render func() (int, error)) error {
	return withParentEvidenceLedgerLock(st, func() error {
		decision, entry, err := decideParentRead(st, surface, digest)
		if err != nil {
			return err
		}
		if decision == parentReadDuplicate {
			recordParentEvidence(st, state.ParentEvidenceRecord{
				Surface: surface, Origin: state.ParentEvidenceOriginStandalone,
				Digest: digest, Outcome: state.ParentEvidenceOutcomeDuplicate,
				Reason: parentEvidenceUnchangedReason, OwnerCallID: entry.OwnerCallID,
			})
			return &DuplicateParentProjectionError{Surface: surface, Digest: digest, OwnerCallID: entry.OwnerCallID}
		}
		written, renderErr := render()
		if renderErr != nil {
			return renderErr
		}
		recordParentEvidence(st, state.ParentEvidenceRecord{
			Surface: surface, Origin: state.ParentEvidenceOriginStandalone,
			Digest: digest, Bytes: written, Outcome: state.ParentEvidenceOutcomeProjected,
		})
		return saveParentEvidenceLedger(st, surface, digest, state.ParentEvidenceOriginStandalone, "")
	})
}
