package controller

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExportEvidenceBoundSectionRecordsMissingTranscriptUnderBoundPrefix(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	fixture.bindSealedAttemptRuntime(t, rolloutTimestampedLine(5010, "claude-attempt-own"))
	if err := os.Remove(filepath.Join(fixture.cfg.ClaudeConfigDir, "projects", "fixture", "session-sealed-worker.jsonl")); err != nil {
		t.Fatal(err)
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	entry := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker")
	if !entry.Missing || entry.Source != evidenceExportSourceTranscript {
		t.Fatalf("bound missing transcript entry = %#v", entry)
	}
	if exportHasEntry(export.Manifest, "live/transcripts/claude/session-sealed-worker") {
		t.Fatal("bound export disclosed a missing transcript under the live prefix")
	}
}

func TestExportEvidenceBoundSectionExcludesPostAttemptSessionRecords(t *testing.T) {
	t.Parallel()
	sealedOptions := func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	}
	transcriptLine := func(seconds int64, line string) string {
		return `{"timestamp":"` + time.Unix(seconds, 0).UTC().Format(time.RFC3339Nano) + `","line":"` + line + `"}` + "\n"
	}

	t.Run("timestamped records after the seal are excluded", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, sealedOptions)
		fixture.bindSealedAttemptRuntime(t,
			transcriptLine(4990, "earlier-task")+
				transcriptLine(5010, "attempt-own")+
				transcriptLine(5100, "attempt-final")+
				transcriptLine(5200, "contaminating-later-task"))
		export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
		if err != nil {
			t.Fatal(err)
		}
		if result.RuntimeStatus != evidenceExportRuntimeCollected || export.Manifest.Runtime.Mode != evidenceExportRuntimeModeBound {
			t.Fatalf("bound sealed export runtime = %#v", export.Manifest.Runtime)
		}
		window := export.Manifest.Runtime.Window
		if window == nil || !window.End.Equal(fixture.seal.SealedAt) || window.EndBasis != evidenceExportBasisAttemptSeal {
			t.Fatalf("bound sealed window = %#v", window)
		}
		entry := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker/0")
		if entry.Unattributed != "" || entry.Window == nil || entry.Window.RecordsBefore != 1 || entry.Window.RecordsAfter != 1 ||
			entry.Window.Basis != evidenceWindowBasisTimestamp {
			t.Fatalf("bound transcript entry = %#v", entry)
		}
		if entry.SHA256 != digestBytes(exportFileByPath(t, export, "bound/transcripts/claude/session-sealed-worker/0")) {
			t.Fatal("bound transcript digest does not match archived bytes")
		}
		for _, file := range export.Files {
			if bytes.Contains(file.Data, []byte("contaminating-later-task")) {
				t.Fatalf("export %s leaked a record appended after the attempt sealed", file.Path)
			}
		}
	})

	t.Run("timestamp-less records are bounded by canonical capture offsets", func(t *testing.T) {
		attemptDedicated := "{\"line\":\"attempt-own\"}\n{\"line\":\"attempt-final\"}\n"
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			sealedOptions(f)
			f.sealedModelWindows = []runtimeTranscriptWindowRecord{{
				SessionID:  "session-sealed-worker",
				TotalBytes: int64(len(attemptDedicated)), StartOffset: 0, EndOffset: int64(len(attemptDedicated)),
				Basis: evidenceWindowBasisCaptureOffsets,
			}}
		})
		fixture.bindSealedAttemptRuntime(t, attemptDedicated+"{\"line\":\"contaminating-later-task\"}\n")
		export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
		if err != nil {
			t.Fatal(err)
		}
		entry := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker/0")
		if entry.Unattributed != "" || entry.Window == nil || entry.Window.Basis != evidenceWindowBasisCaptureOffsets ||
			entry.Window.RecordsAfter != 1 {
			t.Fatalf("capture-offset bound entry = %#v", entry)
		}
		if string(exportFileByPath(t, export, "bound/transcripts/claude/session-sealed-worker/0")) != attemptDedicated {
			t.Fatalf("capture-offset bound bytes = %q", exportFileByPath(t, export, "bound/transcripts/claude/session-sealed-worker/0"))
		}
	})

	t.Run("timestamp-less records without canonical offsets stay unattributed", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, sealedOptions)
		fixture.bindSealedAttemptRuntime(t, "{\"line\":\"attempt-own\"}\n{\"line\":\"contaminating-later-task\"}\n")
		export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
		if err != nil {
			t.Fatal(err)
		}
		entry := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker/0")
		if entry.Unattributed == "" || entry.SHA256 != "" {
			t.Fatalf("unattributed bound entry = %#v", entry)
		}
		if export.Manifest.Runtime.Unattributed == nil {
			t.Fatalf("runtime section hides unattributed transcripts: %#v", export.Manifest.Runtime)
		}
		for _, file := range export.Files {
			if file.Path == "bound/transcripts/claude/session-sealed-worker/0" {
				t.Fatal("unattributed transcript bytes were archived without an attempt-dedicated range")
			}
		}
	})
}

func rolloutTimestampedLine(seconds int64, line string) string {
	return `{"timestamp":"` + time.Unix(seconds, 0).UTC().Format(time.RFC3339Nano) + `","line":"` + line + `"}` + "\n"
}

func rolloutMetaLine(seconds int64, id, parentThreadID string, guardian bool) string {
	source := ""
	if guardian {
		source = `,"source":{"subagent":{"other":"guardian"}}`
	}
	parent := ""
	if parentThreadID != "" {
		parent = `,"parent_thread_id":"` + parentThreadID + `"`
	}
	return `{"type":"session_meta","timestamp":"` + time.Unix(seconds, 0).UTC().Format(time.RFC3339Nano) + `","payload":{"id":"` + id + `"` + parent + source + `}}` + "\n"
}

func writeEvidenceExportRollout(t *testing.T, codexHome, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(codexHome, "sessions", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f *evidenceExportFixture) bindRuntimeParentIdentity(t *testing.T, threadID, sessionID string) *state.StateStore {
	t.Helper()
	runtimeCfg := f.cfg
	runtimeCfg.RepoHash = f.runtimeRepoHash()
	runtime, err := state.NewStateStore(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetParentCodexIdentity(threadID, sessionID, func() *state.SessionLimitReading { return nil }); err != nil {
		t.Fatal(err)
	}
	return runtime
}

func writeParentGuardianRolloutSet(t *testing.T, fixture *evidenceExportFixture, attemptStart int64) (string, []byte) {
	t.Helper()
	parentBytes := []byte(strings.Join([]string{
		strings.TrimSuffix(rolloutMetaLine(attemptStart-16, evidenceExportParentThreadID, "", false), "\n"),
		rolloutTimestampedLine(attemptStart-11, "before-attempt-window"),
		rolloutTimestampedLine(attemptStart+9, "attempt-own"),
		rolloutTimestampedLine(attemptStart+99, "attempt-final"),
		rolloutTimestampedLine(attemptStart+199, "after-seal"),
	}, "\n") + "\n")
	parentPath := writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, evidenceExportParentThreadID+".jsonl", parentBytes)
	earlyGuardian := []byte(strings.TrimSuffix(rolloutMetaLine(attemptStart-51, evidenceExportEarlyGuardianID, evidenceExportParentThreadID, true), "\n") + "\n" +
		rolloutTimestampedLine(attemptStart-41, "guardian-before-attempt"))
	writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, evidenceExportEarlyGuardianID+".jsonl", earlyGuardian)
	activeGuardian := []byte(strings.TrimSuffix(rolloutMetaLine(attemptStart+9, evidenceExportActiveGuardianID, evidenceExportParentThreadID, true), "\n") + "\n" +
		rolloutTimestampedLine(attemptStart+14, "guardian-attempt-own") +
		rolloutTimestampedLine(attemptStart+199, "guardian-after-seal"))
	writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, evidenceExportActiveGuardianID+".jsonl", activeGuardian)
	writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, evidenceExportForeignThreadID+".jsonl",
		[]byte(rolloutMetaLine(attemptStart+5, evidenceExportForeignThreadID, "", false)+rolloutTimestampedLine(attemptStart+6, "foreign-parent-record")))
	writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, "guardian-foreign.jsonl",
		[]byte(rolloutMetaLine(attemptStart+5, "guardian-foreign", evidenceExportForeignThreadID, true)+rolloutTimestampedLine(attemptStart+6, "foreign-guardian-record")))
	return parentPath, parentBytes
}

func TestExportEvidenceBoundSectionCollectsParentAndGuardianRawLogs(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	fixture.bindSealedAttemptRuntime(t,
		rolloutTimestampedLine(4990, "claude-earlier-task")+
			rolloutTimestampedLine(5010, "claude-attempt-own")+
			rolloutTimestampedLine(5200, "claude-after-seal"))
	fixture.bindRuntimeParentIdentity(t, evidenceExportParentThreadID, evidenceExportParentSessionID)
	parentPath, parentBytes := writeParentGuardianRolloutSet(t, fixture, 5001)
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeStatus != evidenceExportRuntimeCollected || export.Manifest.Runtime.Mode != evidenceExportRuntimeModeBound {
		t.Fatalf("bound sealed export runtime = %#v", export.Manifest.Runtime)
	}
	parent := exportEntryByPath(t, export.Manifest, "bound/transcripts/parent/"+evidenceExportParentThreadID+"/0")
	if parent.SHA256 != digestBytes(parentBytes) || parent.Bytes != int64(len(parentBytes)) {
		t.Fatalf("parent raw entry = %#v", parent)
	}
	if string(exportFileByPath(t, export, "bound/transcripts/parent/"+evidenceExportParentThreadID+"/0")) != string(parentBytes) {
		t.Fatal("parent rollout was trimmed instead of archived as the associated raw log")
	}
	if parent.Window == nil || parent.Window.Basis != evidenceWindowBasisRawLog || parent.Window.TotalBytes != int64(len(parentBytes)) {
		t.Fatalf("parent raw window = %#v", parent.Window)
	}
	for _, path := range []string{
		"bound/transcripts/guardian/" + evidenceExportEarlyGuardianID,
		"bound/transcripts/guardian/" + evidenceExportActiveGuardianID,
	} {
		guardian := exportEntryByPath(t, export.Manifest, path)
		if guardian.Window == nil || guardian.Window.Basis != evidenceWindowBasisRawLog || guardian.Missing || guardian.Unreadable != "" {
			t.Fatalf("guardian raw entry %s = %#v", path, guardian)
		}
	}
	if !bytes.Contains(exportFileByPath(t, export, "bound/transcripts/guardian/"+evidenceExportEarlyGuardianID), []byte("guardian-before-attempt")) {
		t.Fatal("export dropped a guardian rollout that ended before the target attempt started")
	}
	if !bytes.Contains(exportFileByPath(t, export, "bound/transcripts/guardian/"+evidenceExportActiveGuardianID), []byte("guardian-after-seal")) {
		t.Fatal("export trimmed guardian records appended after the attempt sealed")
	}
	for _, file := range export.Files {
		if bytes.Contains(file.Data, []byte("foreign-parent-record")) || bytes.Contains(file.Data, []byte("foreign-guardian-record")) {
			t.Fatalf("export absorbed a rollout outside the parent association: %s", file.Path)
		}
	}
	claude := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker/0")
	if claude.Window == nil || claude.Window.Basis != evidenceWindowBasisTimestamp || claude.Window.RecordsBefore != 1 || claude.Window.RecordsAfter != 1 {
		t.Fatalf("model transcript kept its attempt-dedicated window = %#v", claude.Window)
	}
	grown := append([]byte(nil), parentBytes...)
	grown = append(grown, []byte(rolloutTimestampedLine(5300, "grown-after-export"))...)
	if err := os.WriteFile(parentPath, grown, 0o600); err != nil {
		t.Fatal(err)
	}
	reexport, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exportFileByPath(t, reexport, "bound/transcripts/parent/"+evidenceExportParentThreadID+"/0"), []byte("grown-after-export")) {
		t.Fatal("re-export did not pick up parent log growth from after the previous bundle")
	}
}

func TestExportEvidenceLiveSectionCollectsFullParentRollout(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	fixture.bindRuntimeParentIdentity(t, evidenceExportParentThreadID, evidenceExportParentSessionID)
	_, parentBytes := writeParentGuardianRolloutSet(t, fixture, 6000)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entry := exportEntryByPath(t, export.Manifest, "live/transcripts/parent/"+evidenceExportParentThreadID+"/0")
	if entry.SHA256 != digestBytes(parentBytes) || entry.Window == nil || entry.Window.Basis != evidenceWindowBasisRawLog {
		t.Fatalf("live parent raw entry = %#v", entry)
	}
	if string(exportFileByPath(t, export, "live/transcripts/parent/"+evidenceExportParentThreadID+"/0")) != string(parentBytes) {
		t.Fatal("live export trimmed the parent rollout instead of archiving the associated raw log")
	}
	if !exportHasEntry(export.Manifest, "live/transcripts/guardian/"+evidenceExportEarlyGuardianID) {
		t.Fatal("live export dropped a guardian rollout that predates the live attempt")
	}
}

func TestScanTranscriptWindowBoundsReusedSessions(t *testing.T) {
	t.Parallel()
	data := []byte("{\"timestamp\":\"2026-10-05T00:00:00Z\",\"n\":1}\n" +
		"{\"timestamp\":\"2026-10-05T12:00:00Z\",\"n\":2}\n" +
		"{\"timestamp\":\"2026-10-06T00:00:00Z\",\"n\":3}\n" +
		"{\"timestamp\":\"2026-10-07T00:00:00Z\",\"n\":4}\n" +
		"{\"timestamp\":\"2026-10-08T00:00:00Z\",\"n\":5}\n")
	window, windowed := scanTranscriptWindow(data, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), nil)
	if window.RecordsBefore != 2 || window.RecordsAfter != 1 {
		t.Fatalf("window records before=%d after=%d", window.RecordsBefore, window.RecordsAfter)
	}
	if window.Basis != evidenceWindowBasisTimestamp || window.TotalBytes != int64(len(data)) {
		t.Fatalf("window = %#v", window)
	}
	if string(windowed) != "{\"timestamp\":\"2026-10-06T00:00:00Z\",\"n\":3}\n{\"timestamp\":\"2026-10-07T00:00:00Z\",\"n\":4}\n" {
		t.Fatalf("windowed bytes = %q", windowed)
	}
	excluded := len("{\"timestamp\":\"2026-10-08T00:00:00Z\",\"n\":5}\n")
	if window.EndOffset != int64(len(data)-excluded) || window.StartOffset <= 0 {
		t.Fatalf("window offsets = %#v", window)
	}
	unattributed, none := scanTranscriptWindow([]byte("{\"line\":1}\n"), time.Now(), time.Time{}, nil)
	if unattributed.Basis != evidenceWindowBasisUnbounded || unattributed.Unattributed == "" || none != nil {
		t.Fatalf("timestamp-less window without capture offsets = %#v bytes = %q", unattributed, none)
	}
	attemptDedicated := "{\"line\":1}\n{\"line\":2}\n"
	grown := []byte(attemptDedicated + "{\"line\":3}\n")
	capture := &runtimeTranscriptWindowRecord{
		TotalBytes: int64(len(attemptDedicated)), StartOffset: 0, EndOffset: int64(len(attemptDedicated)),
		Basis: evidenceWindowBasisCaptureOffsets,
	}
	offsetWindow, offsetBytes := scanTranscriptWindow(grown, time.Time{}, time.Time{}, capture)
	if offsetWindow.Basis != evidenceWindowBasisCaptureOffsets || offsetWindow.Unattributed != "" ||
		offsetWindow.RecordsAfter != 1 || string(offsetBytes) != attemptDedicated {
		t.Fatalf("capture-offset window = %#v bytes = %q", offsetWindow, offsetBytes)
	}
	stale := &runtimeTranscriptWindowRecord{TotalBytes: int64(len(grown)), StartOffset: 0, EndOffset: int64(len(grown)) + 1}
	staleWindow, staleBytes := scanTranscriptWindow(grown, time.Time{}, time.Time{}, stale)
	if staleWindow.Unattributed == "" || staleBytes != nil {
		t.Fatalf("stale capture-offset window = %#v bytes = %q", staleWindow, staleBytes)
	}
}
