package state

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
)

type ParentEvidenceRecord struct {
	Version     int       `json:"version"`
	Time        time.Time `json:"time"`
	OwnerCallID string    `json:"owner_call_id"`
	Surface     string    `json:"surface"`
	Origin      string    `json:"origin"`
	Digest      string    `json:"digest,omitempty"`
	Bytes       int       `json:"bytes"`
	TokenProxy  int       `json:"token_proxy"`
	Outcome     string    `json:"outcome"`
	Reason      string    `json:"reason,omitempty"`
	Locator     string    `json:"locator,omitempty"`
	TaskID      string    `json:"task_id,omitempty"`
}
type ParentEvidenceLedgerEntry struct {
	Surface     string    `json:"surface"`
	Digest      string    `json:"digest"`
	Origin      string    `json:"origin"`
	OwnerCallID string    `json:"owner_call_id"`
	ProjectedAt time.Time `json:"projected_at"`
}
type parentEvidenceLedgerFile struct {
	Version int                                    `json:"version"`
	TaskID  string                                 `json:"task_id"`
	Lease   int64                                  `json:"lease"`
	Entries map[string][]ParentEvidenceLedgerEntry `json:"entries"`
}
type ParentEvidenceSummary struct {
	Records             int            `json:"records"`
	OwnerCalls          int            `json:"owner_calls"`
	ProjectedBytes      int            `json:"projected_bytes"`
	ProjectedTokenProxy int            `json:"projected_token_proxy"`
	UnchangedCount      int            `json:"unchanged_count"`
	UnchangedBytes      int            `json:"unchanged_bytes"`
	DuplicateRejections int            `json:"duplicate_rejections"`
	Refinements         int            `json:"refinements"`
	Errors              int            `json:"errors"`
	BySurface           map[string]int `json:"by_surface"`
}
type parentEvidenceLedgerScopeID struct {
	taskID string
	lease  int64
}

const (
	ParentEvidenceSurfaceAuthority         = "authority"
	ParentEvidenceSurfaceHandoff           = "handoff"
	ParentEvidenceSurfaceHandoffRecovery   = "handoff-recovery"
	ParentEvidenceSurfaceStatus            = "status"
	ParentEvidenceSurfaceSearch            = "search"
	ParentEvidenceSurfaceDiff              = "diff"
	ParentEvidenceSurfaceSource            = "source"
	ParentEvidenceSurfaceValidations       = "validations"
	ParentEvidenceSurfaceEvidenceTelemetry = "evidence-telemetry"
)

const (
	ParentEvidenceOutcomeProjected  = "projected"
	ParentEvidenceOutcomeUnchanged  = "unchanged"
	ParentEvidenceOutcomeRefinement = "refinement_required"
	ParentEvidenceOutcomeDuplicate  = "rejected_duplicate"
	ParentEvidenceOutcomeError      = "error"
)

const (
	ParentEvidenceOriginStandalone = "standalone"
	ParentEvidenceOriginEvidence   = "evidence"
)

const parentEvidenceFile = "parent-evidence.jsonl"
const parentEvidenceLedgerPath = "parent-evidence-ledger.json"
const parentEvidenceLeasePath = "parent-evidence-lease"
const ParentEvidenceLedgerLockFile = "parent-evidence-ledger.lock"
const parentEvidenceRecordVersion = 1
const parentEvidenceLedgerVersion = 2

func ParentEvidenceTokenProxy(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

func WarnParentEvidenceLedgerSkip(err error) {
	writeStatsWarningEvent("parent_evidence_ledger", "parent evidence ledgerの更新に失敗したため重複検出だけが無効になります", err)
}

func (s *StateStore) Present() bool {
	if s == nil {
		return false
	}
	info, err := os.Stat(s.dir)
	return err == nil && info.IsDir()
}

func (s *StateStore) RecordParentEvidence(record ParentEvidenceRecord) {
	record.Version = parentEvidenceRecordVersion
	if record.Time.IsZero() {
		record.Time = time.Now().UTC()
	}
	if record.TokenProxy == 0 {
		record.TokenProxy = ParentEvidenceTokenProxy(record.Bytes)
	}
	data, err := json.Marshal(record)
	if err != nil {
		warnStatsFailure("parent evidence telemetryのJSON化", err)
		return
	}
	path := s.Path(parentEvidenceFile)
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		warnStatsFailure("parent evidence telemetry directoryの確認", err)
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		warnStatsFailure("parent evidence telemetryの追記", err)
		return
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		warnStatsFailure("parent evidence telemetryの権限設定", err)
		return
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		warnStatsFailure("parent evidence telemetryの追記", err)
		return
	}
	if err := file.Close(); err != nil {
		warnStatsFailure("parent evidence telemetryのclose", err)
	}
}

func (s *StateStore) ReadParentEvidence() ([]ParentEvidenceRecord, error) {
	data, err := os.ReadFile(s.Path(parentEvidenceFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []ParentEvidenceRecord
	for _, line := range splitJSONLines(data) {
		var record ParentEvidenceRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("parent evidence telemetryを読めません: %w", err)
		}
		records = append(records, record)
	}
	return records, nil
}

func splitJSONLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for index := 0; index < len(data); index++ {
		if data[index] == '\n' {
			segment := data[start:index]
			start = index + 1
			if len(segment) > 0 {
				lines = append(lines, segment)
			}
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func SummarizeParentEvidence(records []ParentEvidenceRecord) ParentEvidenceSummary {
	ownerCalls := make(map[string]struct{})
	summary := ParentEvidenceSummary{BySurface: map[string]int{}}
	for _, record := range records {
		summary.Records++
		if record.OwnerCallID != "" {
			ownerCalls[record.OwnerCallID] = struct{}{}
		}
		summary.BySurface[record.Surface]++
		switch record.Outcome {
		case ParentEvidenceOutcomeUnchanged:
			summary.UnchangedCount++
			summary.UnchangedBytes += record.Bytes
		case ParentEvidenceOutcomeDuplicate:
			summary.DuplicateRejections++
		case ParentEvidenceOutcomeRefinement:
			summary.Refinements++
		case ParentEvidenceOutcomeError:
			summary.Errors++
		case ParentEvidenceOutcomeProjected:
			summary.ProjectedBytes += record.Bytes
			summary.ProjectedTokenProxy += record.TokenProxy
		}
	}
	summary.OwnerCalls = len(ownerCalls)
	return summary
}

func (s *StateStore) SaveParentEvidenceLedgerEntry(entry ParentEvidenceLedgerEntry) error {
	scope, err := s.parentEvidenceLedgerScope()
	if err != nil {
		return err
	}
	ledger, err := s.loadParentEvidenceLedger()
	if err != nil {
		return err
	}
	if ledger.Entries == nil {
		ledger.Entries = map[string][]ParentEvidenceLedgerEntry{}
	}
	entry.ProjectedAt = time.Now().UTC()
	appended := false
	entries := ledger.Entries[entry.Surface]
	for index := range entries {
		if entries[index].Digest == entry.Digest {
			entries[index] = entry
			appended = true
			break
		}
	}
	if !appended {
		entries = append(entries, entry)
	}
	ledger.Entries[entry.Surface] = entries
	ledger.Version = parentEvidenceLedgerVersion
	ledger.TaskID = scope.taskID
	ledger.Lease = scope.lease
	data, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("parent evidence ledgerをJSON化できません: %w", err)
	}
	return writeFileAtomic(s.Path(parentEvidenceLedgerPath), append(data, '\n'), 0o600)
}

func (s *StateStore) ParentEvidenceDelivered(surface string, digest string) (ParentEvidenceLedgerEntry, bool, error) {
	ledger, err := s.loadParentEvidenceLedger()
	if err != nil {
		return ParentEvidenceLedgerEntry{}, false, err
	}
	for _, entry := range ledger.Entries[surface] {
		if entry.Digest == digest {
			return entry, true, nil
		}
	}
	return ParentEvidenceLedgerEntry{}, false, nil
}

func (s *StateStore) ClearParentEvidenceLedger() error {
	if err := removeStatePath(s.Path(parentEvidenceLedgerPath)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("parent evidence ledgerを削除できません: %w", err)
	}
	return nil
}

func (s *StateStore) parentEvidenceLedgerScope() (parentEvidenceLedgerScopeID, error) {
	lease, err := s.ParentEvidenceLeaseEpoch()
	if err != nil {
		return parentEvidenceLedgerScopeID{}, err
	}
	return parentEvidenceLedgerScopeID{taskID: s.ReadOr("task.id", ""), lease: lease}, nil
}

func (s *StateStore) loadParentEvidenceLedger() (parentEvidenceLedgerFile, error) {
	scope, err := s.parentEvidenceLedgerScope()
	if err != nil {
		return parentEvidenceLedgerFile{}, err
	}
	data, err := os.ReadFile(s.Path(parentEvidenceLedgerPath))
	if err != nil {
		if os.IsNotExist(err) {
			return parentEvidenceLedgerFile{}, nil
		}
		return parentEvidenceLedgerFile{}, err
	}
	var ledger parentEvidenceLedgerFile
	if err := json.Unmarshal(data, &ledger); err != nil {
		return parentEvidenceLedgerFile{}, fmt.Errorf("parent evidence ledgerを読めません: %w", err)
	}
	if ledger.Version != parentEvidenceLedgerVersion {
		return parentEvidenceLedgerFile{}, nil
	}
	if ledger.TaskID != scope.taskID || ledger.Lease != scope.lease {
		return parentEvidenceLedgerFile{}, nil
	}
	return ledger, nil
}

func (s *StateStore) ParentEvidenceLeaseEpoch() (int64, error) {
	data, err := os.ReadFile(s.Path(parentEvidenceLeasePath))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("parent evidence leaseを読めません: %w", err)
	}
	epoch, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if parseErr != nil {
		return 0, fmt.Errorf("parent evidence leaseを読めません: %w", parseErr)
	}
	return epoch, nil
}

func (s *StateStore) AdvanceParentEvidenceLease() error {
	lock, err := repolock.AcquireWait(s.Path(ParentEvidenceLedgerLockFile))
	if err != nil {
		return fmt.Errorf("parent evidence ledger lockを取得できません: %w", err)
	}
	defer func() { _ = lock.Close() }()
	return s.advanceParentEvidenceLeaseUnlocked()
}

func (s *StateStore) RotateParentEvidenceLease() error {
	lock, err := repolock.AcquireWait(s.Path(ParentEvidenceLedgerLockFile))
	if err != nil {
		return fmt.Errorf("parent evidence ledger lockを取得できません: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := s.ClearParentEvidenceLedger(); err != nil {
		return err
	}
	return s.advanceParentEvidenceLeaseUnlocked()
}

func (s *StateStore) advanceParentEvidenceLeaseUnlocked() error {
	epoch, err := s.ParentEvidenceLeaseEpoch()
	if err != nil {
		return err
	}
	next := epoch + 1
	if err := writeFileAtomic(s.Path(parentEvidenceLeasePath), []byte(strconv.FormatInt(next, 10)+"\n"), 0o600); err != nil {
		WarnParentEvidenceLeaseSkip(err)
		return err
	}
	return nil
}

func WarnParentEvidenceLeaseSkip(err error) {
	writeStatsWarningEvent("parent_evidence_lease", "parent evidence leaseの更新に失敗したため旧leaseの配信記録が残る場合があります", err)
}
