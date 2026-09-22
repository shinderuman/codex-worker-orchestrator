package parentevidence

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

type DuplicateProjectionError struct {
	Surface     string
	Digest      string
	OwnerCallID string
}

type ReadDecision int

type ReadScope struct {
	storePresent bool
	taskID       string
	taskStatus   state.TaskStatus
	leaseEpoch   int64
}

type RenderResult struct {
	Bytes   int
	Outcome string
	Reason  string
}

const BatchCommand = "glm-parent-action evidence <manifest.json>"

const UnchangedReason = "identical projection was already delivered within this decision lease"

const (
	ReadServe ReadDecision = iota
	ReadDuplicate
)

func (e *DuplicateProjectionError) Error() string {
	return fmt.Sprintf(
		"parent projection for surface %q was already delivered by evidence owner call %s; use %s instead of repeating the fine-grained read",
		e.Surface, e.OwnerCallID, BatchCommand,
	)
}

func LeaseActive(st *state.StateStore) bool {
	return StatusHasLease(st.TaskStatus())
}

func StatusHasLease(status state.TaskStatus) bool {
	switch status {
	case state.TaskStatusWaitingDecision, state.TaskStatusWaitingSolReview, state.TaskStatusParked:
		return true
	default:
		return false
	}
}

func CaptureReadScope(st *state.StateStore) (ReadScope, error) {
	if !StorePresent(st) {
		return ReadScope{}, nil
	}
	epoch, err := st.ParentEvidenceLeaseEpoch()
	if err != nil {
		return ReadScope{}, err
	}
	return ReadScope{
		storePresent: true,
		taskID:       st.ReadOr("task.id", ""),
		taskStatus:   st.TaskStatus(),
		leaseEpoch:   epoch,
	}, nil
}

func ValidateReadScope(st *state.StateStore, scope ReadScope) error {
	present := StorePresent(st)
	if present != scope.storePresent {
		return fmt.Errorf("parent evidence scope changed during projection; request fresh evidence")
	}
	if !present {
		return nil
	}
	epoch, err := st.ParentEvidenceLeaseEpoch()
	if err != nil {
		return err
	}
	if epoch != scope.leaseEpoch || st.ReadOr("task.id", "") != scope.taskID || st.TaskStatus() != scope.taskStatus {
		return fmt.Errorf("parent evidence scope changed during projection; request fresh evidence")
	}
	return nil
}

func Digest(value any) (string, int) {
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

func StringDigest(parts ...string) string {
	hasher := sha256.New()
	for _, part := range parts {
		hasher.Write([]byte(part))
		hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func StorePresent(st *state.StateStore) bool {
	return st != nil && st.Present()
}

func Record(st *state.StateStore, record state.ParentEvidenceRecord) {
	if !StorePresent(st) {
		return
	}
	if record.TaskID == "" {
		record.TaskID = st.ReadOr("task.id", "")
	}
	st.RecordParentEvidence(record)
}

func DecideRead(st *state.StateStore, surface, digest string) (ReadDecision, state.ParentEvidenceLedgerEntry, error) {
	if !StorePresent(st) || !LeaseActive(st) {
		return ReadServe, state.ParentEvidenceLedgerEntry{}, nil
	}
	entry, delivered, err := st.ParentEvidenceDelivered(surface, digest)
	if err != nil {
		return ReadServe, entry, err
	}
	if !delivered {
		return ReadServe, state.ParentEvidenceLedgerEntry{}, nil
	}
	return ReadDuplicate, entry, nil
}

func WithLedgerLock(st *state.StateStore, body func() error) error {
	if !StorePresent(st) || !LeaseActive(st) {
		return body()
	}
	lock, err := repolock.AcquireWait(st.Path(state.ParentEvidenceLedgerLockFile))
	if err != nil {
		return fmt.Errorf("parent evidence ledger lockを取得できません: %w", err)
	}
	defer func() { _ = lock.Close() }()
	return body()
}

func WithReadScopeLock(st *state.StateStore, scope ReadScope, body func() error) error {
	if !StorePresent(st) || (!StatusHasLease(scope.taskStatus) && !LeaseActive(st)) {
		return body()
	}
	lock, err := repolock.AcquireWait(st.Path(state.ParentEvidenceLedgerLockFile))
	if err != nil {
		return fmt.Errorf("parent evidence ledger lockを取得できません: %w", err)
	}
	defer func() { _ = lock.Close() }()
	return body()
}

func WriteMeasuredJSON(stdout io.Writer, value any) (int, error) {
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

func SaveLedger(st *state.StateStore, surface, digest, origin, ownerCallID string) error {
	if !StorePresent(st) || !LeaseActive(st) {
		return nil
	}
	if err := st.SaveParentEvidenceLedgerEntry(state.ParentEvidenceLedgerEntry{
		Surface: surface, Digest: digest, Origin: origin, OwnerCallID: ownerCallID,
	}); err != nil {
		return fmt.Errorf("parent evidence delivery claimを保存できません: %w", err)
	}
	return nil
}

func FinishReadInScope(st *state.StateStore, scope ReadScope, surface, digest string, render func() (int, error)) error {
	return FinishReadInScopeResult(st, scope, surface, digest, func() (RenderResult, error) {
		written, err := render()
		return RenderResult{Bytes: written, Outcome: state.ParentEvidenceOutcomeProjected}, err
	})
}

func FinishReadInScopeResult(st *state.StateStore, scope ReadScope, surface, digest string, render func() (RenderResult, error)) error {
	return WithReadScopeLock(st, scope, func() error {
		if err := ValidateReadScope(st, scope); err != nil {
			return err
		}
		decision, entry, err := DecideRead(st, surface, digest)
		if err != nil {
			return err
		}
		if decision == ReadDuplicate {
			Record(st, state.ParentEvidenceRecord{
				Surface: surface, Origin: state.ParentEvidenceOriginStandalone,
				Digest: digest, Outcome: state.ParentEvidenceOutcomeDuplicate,
				Reason: UnchangedReason, OwnerCallID: entry.OwnerCallID,
			})
			return &DuplicateProjectionError{Surface: surface, Digest: digest, OwnerCallID: entry.OwnerCallID}
		}
		rendered, renderErr := render()
		if renderErr != nil {
			return renderErr
		}
		if err := SaveLedger(st, surface, digest, state.ParentEvidenceOriginStandalone, ""); err != nil {
			return err
		}
		Record(st, state.ParentEvidenceRecord{
			Surface: surface, Origin: state.ParentEvidenceOriginStandalone,
			Digest: digest, Bytes: rendered.Bytes, Outcome: rendered.Outcome, Reason: rendered.Reason,
		})
		return nil
	})
}
