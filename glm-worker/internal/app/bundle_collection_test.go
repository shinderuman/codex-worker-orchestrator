package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBundleDistinguishesCollectionStatusFromCoverage(t *testing.T) {
	cases := []struct {
		name           string
		status         state.TaskStatus
		withTranscript bool
		wantCoverage   string
		wantReasons    []string
	}{
		{
			name:           "current-rate-limited",
			status:         state.TaskStatusRateLimited,
			withTranscript: true,
			wantCoverage:   bundleCoverageOpen,
			wantReasons:    []string{"task-current", "task-status:rate-limited"},
		},
		{
			name:           "current-with-missing-transcript",
			status:         state.TaskStatusActive,
			withTranscript: false,
			wantCoverage:   bundleCoveragePartial,
			wantReasons:    []string{"task-current", "task-status:active", "missing-evidence"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, st := newBundleTestState(t)
			taskID, err := st.StartNewTask()
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(tc.status); err != nil {
				t.Fatal(err)
			}
			st.RecordModelCall(state.WorkerRole, "opus")
			writeBundleModelCall(t, st, taskID, "session-worker", state.WorkerRole, "worker-new")
			writeBundleEvent(t, st, taskID, "session-worker")
			writeBundleFile(t, st.RoundLogPath(taskID), "round\n")
			writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")
			if tc.withTranscript {
				writeClaudeTranscript(t, cfg, "project-a", "session-worker", "worker transcript\n")
			}

			var stdout bytes.Buffer
			if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
				t.Fatal(err)
			}
			var output bundleOutput
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			if output.EvidenceStatus != "complete" && tc.withTranscript {
				t.Fatalf("evidence status = %s", output.EvidenceStatus)
			}
			if output.Coverage != tc.wantCoverage {
				t.Fatalf("coverage = %s want %s", output.Coverage, tc.wantCoverage)
			}
			if !slices.Equal(output.CoverageReasons, tc.wantReasons) {
				t.Fatalf("coverage reasons = %v want %v", output.CoverageReasons, tc.wantReasons)
			}

			archive := readBundleArchive(t, output.ArchivePath)
			var manifest bundleManifest
			if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Coverage != tc.wantCoverage || !slices.Equal(manifest.CoverageReasons, tc.wantReasons) {
				t.Fatalf("manifest coverage = %s/%v", manifest.Coverage, manifest.CoverageReasons)
			}
			if manifest.CollectionIndex != bundleCollectionEntryPath {
				t.Fatalf("collection index = %q", manifest.CollectionIndex)
			}
			if !slices.Contains(manifest.Included, bundleCollectionEntryPath) {
				t.Fatalf("included = %v", manifest.Included)
			}
			if _, ok := archive[bundleCollectionEntryPath]; !ok {
				t.Fatal("collection.jsonがarchiveへ含まれていません")
			}
			if _, ok := archive["task/lifecycle/"+taskID+".jsonl"]; !ok {
				t.Fatal("lifecycle logがarchiveへ含まれていません")
			}
		})
	}
}

func TestBundleClosedCoverageForCompleteArchivedTask(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, taskID, "session-old", state.WorkerRole, "worker-new")
	writeClaudeTranscript(t, cfg, "old-project", "session-old", "{\"version\":\"2.1.226\"}\n")
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version:      1,
		TaskID:       taskID,
		Seq:          1,
		ReviewNumber: 1,
		WorkerPhase:  "worker-new",
		CapturedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle, Payload: taskID}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.EvidenceStatus != "complete" || output.Coverage != bundleCoverageClosed || len(output.CoverageReasons) != 0 {
		t.Fatalf("output = %s/%s/%v", output.EvidenceStatus, output.Coverage, output.CoverageReasons)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	index := readBundleCollectionIndex(t, archive)
	telemetry := collectionEntryOf(t, index, "task/telemetry/"+taskID+".jsonl")
	if telemetry.InProgress {
		t.Fatal("完結taskの証跡がin-progressとして記録されました")
	}
	lifecycle := collectionEntryOf(t, index, "task/lifecycle/"+taskID+".jsonl")
	if lifecycle.Records != 2 || lifecycle.LastEventAt == "" {
		t.Fatalf("lifecycle index = %#v", lifecycle)
	}
}

func TestBundleCollectionIndexMatchesArchivedBytes(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, taskID, "session-worker", state.WorkerRole, "worker-new")
	writeBundleEvent(t, st, taskID, "session-worker")
	writeBundleFile(t, st.TaskLiveStatusPath(taskID), "{}\n")
	for seq := 1; seq <= 2; seq++ {
		if err := st.AppendRoundRecord(state.RoundRecord{
			Version:      1,
			TaskID:       taskID,
			Seq:          seq,
			ReviewNumber: seq,
			WorkerPhase:  "worker-new",
			CapturedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")
	writeClaudeTranscript(t, cfg, "project-a", "session-worker", "line-1\nline-2\n")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	index := readBundleCollectionIndex(t, archive)
	verifyBundleCollectionIdentity(t, archive, index)

	rounds := collectionEntryOf(t, index, "task/rounds/"+taskID+".jsonl")
	if rounds.Records != 2 || rounds.LastEventAt == "" {
		t.Fatalf("rounds index = %#v", rounds)
	}
	telemetry := collectionEntryOf(t, index, "task/telemetry/"+taskID+".jsonl")
	if telemetry.Records != 1 || telemetry.LastEventAt == "" {
		t.Fatalf("telemetry index = %#v", telemetry)
	}
	if !telemetry.InProgress {
		t.Fatal("current taskのtelemetryがin-progressではありません")
	}
	transcript := collectionEntryOf(t, index, "claude-transcripts/session-worker.jsonl")
	if !transcript.InProgress || transcript.SourceModifiedAt == "" {
		t.Fatalf("transcript index = %#v", transcript)
	}
	authority := collectionEntryOf(t, index, "current-state/repository-authority/IMPLEMENTATION_RULES.md")
	if authority.InProgress {
		t.Fatalf("snapshot evidenceがin-progressとして記録されました: %#v", authority)
	}
}

func TestBundleCompleteTaskWithTrailingFragmentIsPartial(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, taskID, "session-old", state.WorkerRole, "worker-new")
	writeClaudeTranscript(t, cfg, "old-project", "session-old", "{\"version\":\"2.1.226\"}\n")
	writeBundleFile(t, st.RoundLogPath(taskID), "{\"captured_at\":\"2026-08-31T00:00:00Z\"}\n")
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	telemetryPath := st.ModelCallLogPath(taskID)
	original, err := os.ReadFile(telemetryPath)
	if err != nil {
		t.Fatal(err)
	}
	fragment := make([]byte, 0, len(original)+32)
	fragment = append(fragment, original...)
	fragment = append(fragment, "{\"completed_at\":\"2026-08-31T0"...)
	if err := os.WriteFile(telemetryPath, fragment, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle, Payload: taskID}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.EvidenceStatus != "incomplete" ||
		!slices.Contains(output.Missing, "task/telemetry/session-association-unreadable") {
		t.Fatalf("断片はsession関連付け読取りも不完全にするべき: %s/%v", output.EvidenceStatus, output.Missing)
	}
	if output.Coverage != bundleCoveragePartial ||
		!slices.Contains(output.CoverageReasons, "jsonl-anomaly") ||
		!slices.Contains(output.CoverageReasons, "missing-evidence") {
		t.Fatalf("coverage = %s/%v", output.Coverage, output.CoverageReasons)
	}
	if !slices.Contains(output.CoverageScope, "evidence-readability") {
		t.Fatalf("coverage scope = %v", output.CoverageScope)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	if !bytes.Equal(archive["task/telemetry/"+taskID+".jsonl"], fragment) {
		t.Fatal("原本bytesが改変されています")
	}
	index := readBundleCollectionIndex(t, archive)
	telemetry := collectionEntryOf(t, index, "task/telemetry/"+taskID+".jsonl")
	if !telemetry.TrailingFragment || telemetry.Records != 1 || telemetry.InvalidRecords != 0 {
		t.Fatalf("telemetry boundary = %#v", telemetry)
	}
	sum := sha256.Sum256(fragment)
	if telemetry.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("fragment付与後のhashが原本と一致しません")
	}
}

func TestBundleLegacyTaskKeepsClosedWithoutInferringSufficiency(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleLegacyModelCall(t, st, taskID, "session-legacy", state.WorkerRole, "worker-new")
	writeClaudeTranscript(t, cfg, "legacy-project", "session-legacy", "{\"version\":\"2.1.226\"}\n")
	writeBundleFile(t, st.RoundLogPath(taskID), "{\"captured_at\":\"2026-08-31T00:00:00Z\"}\n")
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(st.TaskLifecycleLogPath(taskID)); err != nil {
		t.Fatal(err)
	}
	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle, Payload: taskID}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Coverage != bundleCoverageClosed {
		t.Fatalf("coverage = %s", output.Coverage)
	}
	if !slices.Contains(output.CoverageReasons, "legacy-evidence:runtime") ||
		!slices.Contains(output.CoverageReasons, "legacy-evidence:lifecycle") {
		t.Fatalf("legacy観測欠損の明示がありません: %v", output.CoverageReasons)
	}
	if !slices.Contains(output.CoverageScope, "evidence-presence") {
		t.Fatalf("coverage scope = %v", output.CoverageScope)
	}
}

func TestBundleInProgressFragmentDoesNotDowngradeOpenCoverage(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, taskID, "session-worker", state.WorkerRole, "worker-new")
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")
	writeClaudeTranscript(t, cfg, "project-a", "session-worker", "{\"version\":\"2.1.226\"}\n")
	writeBundleFile(t, st.RoundLogPath(taskID), "{\"captured_at\":\"2026-08-31T00:00:00Z\"}\n{\"captured_at\":\"2026-08-31T0")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Coverage != bundleCoverageOpen || slices.Contains(output.CoverageReasons, "jsonl-anomaly") {
		t.Fatalf("in-progress証跡の断片でopen以外へ落ちました: %s/%v", output.Coverage, output.CoverageReasons)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	index := readBundleCollectionIndex(t, archive)
	rounds := collectionEntryOf(t, index, "task/rounds/"+taskID+".jsonl")
	if !rounds.InProgress || !rounds.TrailingFragment || rounds.Records != 1 {
		t.Fatalf("in-progress rounds boundary = %#v", rounds)
	}
}

func TestBundleReportsUnreadableEvidenceSeparatelyFromMissing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("rootはpermission制約を観測できません")
	}
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, taskID, "session-worker", state.WorkerRole, "worker-new")
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")
	telemetryDir := filepath.Dir(st.ModelCallLogPath(taskID))
	if err := os.Chmod(telemetryDir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(telemetryDir, 0o700) }()

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Unreadable) == 0 || !slices.Contains(output.Unreadable, "task/telemetry/"+taskID+".jsonl") {
		t.Fatalf("unreadable = %v", output.Unreadable)
	}
	if slices.Contains(output.Missing, "task/telemetry/"+taskID+".jsonl") {
		t.Fatalf("unreadable証跡がmissingへ分類されました: %v", output.Missing)
	}
	if output.Coverage != bundleCoveragePartial || !slices.Contains(output.CoverageReasons, "unreadable-evidence") {
		t.Fatalf("coverage = %s/%v", output.Coverage, output.CoverageReasons)
	}
}

func readBundleCollectionIndex(t *testing.T, archive map[string][]byte) bundleCollectionIndex {
	t.Helper()
	var index bundleCollectionIndex
	if err := json.Unmarshal(archive[bundleCollectionEntryPath], &index); err != nil {
		t.Fatal(err)
	}
	return index
}

func verifyBundleCollectionIdentity(t *testing.T, archive map[string][]byte, index bundleCollectionIndex) {
	t.Helper()
	if len(index.Entries) != len(archive)-2 {
		t.Fatalf("index entries = %d archive = %d", len(index.Entries), len(archive))
	}
	for name, data := range archive {
		if name == bundleCollectionEntryPath || name == "manifest.json" || strings.HasSuffix(name, ".md") {
			continue
		}
		entry := collectionEntryOf(t, index, name)
		sum := sha256.Sum256(data)
		if entry.SHA256 != hex.EncodeToString(sum[:]) || entry.Bytes != int64(len(data)) {
			t.Fatalf("collection identity mismatch for %s: %#v", name, entry)
		}
		if entry.CollectedAt == "" {
			t.Fatalf("collected at is missing for %s", name)
		}
	}
}

func collectionEntryOf(t *testing.T, index bundleCollectionIndex, name string) bundleCollectedEntry {
	t.Helper()
	for _, entry := range index.Entries {
		if entry.Path == name {
			return entry
		}
	}
	t.Fatalf("collection entry %sがありません: %#v", name, index.Entries)
	return bundleCollectedEntry{}
}

func TestBundleMeasureRecordsJSONLBoundaries(t *testing.T) {
	var buffer bytes.Buffer
	measure := newBundleEntryMeasure("task/events/demo.jsonl")
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	lines := []byte("{\"timestamp\":\"" + timestamp + "\"}\nnot-json\n\n{\"timestamp\":\"" + timestamp + "\"}")
	if _, err := measure.WriteTo(&buffer, lines); err != nil {
		t.Fatal(err)
	}
	collected := bundleCollectedEntry{}
	measure.apply(&collected)
	if collected.Records != 2 {
		t.Fatalf("records = %d", collected.Records)
	}
	if collected.InvalidRecords != 1 || collected.TrailingFragment {
		t.Fatalf("invalid = %d fragment = %v", collected.InvalidRecords, collected.TrailingFragment)
	}
	if collected.LastEventAt != timestamp {
		t.Fatalf("last event at = %q", collected.LastEventAt)
	}
	sum := sha256.Sum256(lines)
	if collected.SHA256 != hex.EncodeToString(sum[:]) || collected.Bytes != int64(len(lines)) {
		t.Fatalf("content identity = %s/%d", collected.SHA256, collected.Bytes)
	}
	if !strings.Contains(buffer.String(), "not-json") {
		t.Fatal("測定がwriter出力を改変しています")
	}
}

func TestBundleMeasureSeparatesTrailingFragmentFromMissingNewline(t *testing.T) {
	complete := []byte("{\"timestamp\":\"2026-08-31T00:00:00Z\"}")
	measure := newBundleEntryMeasure("task/lifecycle/demo.jsonl")
	if _, err := measure.WriteTo(&bytes.Buffer{}, complete); err != nil {
		t.Fatal(err)
	}
	collected := bundleCollectedEntry{}
	measure.apply(&collected)
	if collected.Records != 1 || collected.TrailingFragment || collected.InvalidRecords != 0 {
		t.Fatalf("newline欠落の完結JSON最終行 = %#v", collected)
	}

	fragmented := []byte("{\"timestamp\":\"2026-08-31T00:00:00Z\"}\n{\"timestamp\":\"2026-08-31T0")
	measure = newBundleEntryMeasure("task/lifecycle/demo.jsonl")
	if _, err := measure.WriteTo(&bytes.Buffer{}, fragmented); err != nil {
		t.Fatal(err)
	}
	collected = bundleCollectedEntry{}
	measure.apply(&collected)
	if collected.Records != 1 || !collected.TrailingFragment || collected.InvalidRecords != 0 {
		t.Fatalf("途中JSON最終行 = %#v", collected)
	}
}

func TestBundleMeasureCountsObservationCapSkipsSeparately(t *testing.T) {
	oversize := append([]byte("{\"timestamp\":\"2026-08-31T00:00:00Z\"}\n"), bytes.Repeat([]byte("x"), bundleMeasureLineLimit+1)...)
	oversize = append(oversize, '\n', '{', '}', '\n')
	measure := newBundleEntryMeasure("task/telemetry/demo.jsonl")
	if _, err := measure.WriteTo(&bytes.Buffer{}, oversize); err != nil {
		t.Fatal(err)
	}
	collected := bundleCollectedEntry{}
	measure.apply(&collected)
	if collected.Records != 2 || collected.DroppedLines != 1 || collected.InvalidRecords != 0 {
		t.Fatalf("観測上限skip = %#v", collected)
	}
	if collected.Bytes != int64(len(oversize)) {
		t.Fatalf("bytes = %d want %d", collected.Bytes, len(oversize))
	}
}

func TestBundleMeasureObservesRuntimeRecordsOnTelemetryOnly(t *testing.T) {
	withRuntime := []byte("{\"completed_at\":\"2026-08-31T00:00:00Z\",\"runtime\":{}}\n{}\n")
	telemetry := newBundleEntryMeasure("task/telemetry/demo.jsonl")
	if _, err := telemetry.WriteTo(&bytes.Buffer{}, withRuntime); err != nil {
		t.Fatal(err)
	}
	collected := bundleCollectedEntry{}
	telemetry.apply(&collected)
	if collected.Records != 2 || collected.RuntimeRecords != 1 {
		t.Fatalf("telemetry runtime観測 = %#v", collected)
	}

	events := newBundleEntryMeasure("task/events/demo.jsonl")
	if _, err := events.WriteTo(&bytes.Buffer{}, withRuntime); err != nil {
		t.Fatal(err)
	}
	collected = bundleCollectedEntry{}
	events.apply(&collected)
	if collected.RuntimeRecords != 0 {
		t.Fatalf("telemetry外のruntime観測 = %#v", collected)
	}
}

func TestBundleCollectionIndexListsCollectorRevisionWhenPresent(t *testing.T) {
	if bundleCollectorRevision() == "" {
		t.Skip("このbuildにはvcs revisionが含まれていません")
	}
	archiveBytes := zipBytesForIndex(t)
	if !bytes.Contains(archiveBytes, []byte(bundleCollectorRevision())) {
		t.Fatal("collector revisionがcollection indexへ含まれていません")
	}
}

func zipBytesForIndex(t *testing.T) []byte {
	t.Helper()
	cfg, st := newBundleTestState(t)
	_, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	for _, file := range reader.File {
		if file.Name == bundleCollectionEntryPath {
			entry, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			_, _ = raw.ReadFrom(entry)
			_ = entry.Close()
		}
	}
	return raw.Bytes()
}
