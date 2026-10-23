package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type CompletionBundleCheck struct {
	SchemaVersion    int                              `json:"schema_version"`
	OperationDigest  string                           `json:"operation_digest"`
	FinalOID         string                           `json:"final_oid"`
	RuntimeTaskID    string                           `json:"runtime_task_id"`
	TaskClockStatus  string                           `json:"task_clock_status"`
	TaskClockBasis   string                           `json:"task_clock_basis"`
	TaskClockStart   time.Time                        `json:"task_clock_start"`
	LifecycleDigest  string                           `json:"lifecycle_digest,omitempty"`
	TelemetryDigest  string                           `json:"telemetry_digest,omitempty"`
	RunStores        []CompletionBundleRunStore       `json:"run_stores"`
	Validations      []CompletionBundleValidationView `json:"validations,omitempty"`
	FinalRuns        []FinalValidationEvidence        `json:"final_validation_runs,omitempty"`
	ModelCallsByRole map[string]int                   `json:"model_calls_by_role,omitempty"`
}

type CompletionBundleValidationView struct {
	Form            string `json:"form"`
	Runs            int    `json:"runs"`
	Pass            int    `json:"pass,omitempty"`
	Fail            int    `json:"fail,omitempty"`
	Interrupted     int    `json:"interrupted,omitempty"`
	DurationMS      int64  `json:"duration_ms,omitempty"`
	WallMS          int64  `json:"wall_ms,omitempty"`
	UnknownDuration int    `json:"unknown_durations,omitempty"`
}

type CompletionBundleRunStore struct {
	Store string                      `json:"store"`
	Runs  []CompletionBundleRunSource `json:"runs"`
}

type CompletionBundleRunSource struct {
	RunID  string `json:"run_id"`
	Digest string `json:"digest"`
}

type bundleFormAggregate struct {
	runs            int
	pass            int
	fail            int
	interrupted     int
	durationMS      int64
	unknownDuration int
	intervals       []workflowInterval
}

type bundleValidationInventory struct {
	byForm  map[string]*bundleFormAggregate
	byRunID map[string]EvidenceAnalysisValidationRun
}

type bundleModelCallTotals struct {
	calls            int
	workMS           int64
	slowestMS        int64
	unknownDurations int
	retried          int
	interrupted      int
	intervals        []workflowInterval
}

type bundleLifecycleBounds struct {
	start       time.Time
	latest      time.Time
	latestTo    string
	completedAt time.Time
}

type bundleRunStoreScan struct {
	store *state.StateStore
	label string
	dir   string
}

const completionBundleCheckEvidenceKind = "completion-bundle-check"

const workflowTaskClockBasis = "runtime-task-lifecycle"

const workflowTaskClockCompleteBasis = "runtime-task-complete"

const completionBundleNoModelRuntimeBasis = "no-model-runtime"

func candidateObservationOperation(c AcceptedCandidate) ExecutionOperation {
	return ExecutionOperation{Terminal: &TerminalMetadataOperation{Record: TerminalTaskRecord{CandidateOID: c.CommitOID, AttemptID: c.AttemptID}}}
}

func (s *Store) prepareCandidateBundleObservation(c AcceptedCandidate) (EvidenceObjectRef, error) {
	validations, err := s.collectFinalValidationEvidence(c)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	op := candidateObservationOperation(c)
	taskID, _, err := s.prepareCompletionRuntime(op)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	check, err := s.completionBundleCheck(op, c, taskID, validations)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	return s.putEvidenceJSON(completionBundleCheckEvidenceKind, c.CandidateID, check)
}

func (s *Store) candidateBundleObservation(op ExecutionOperation, c AcceptedCandidate, taskID string, validations []FinalValidationEvidence) (CompletionBundleCheck, error) {
	var check CompletionBundleCheck
	if c.BundleCheckRef == nil {
		return check, fmt.Errorf("candidate lacks its pre-promotion Bundle observation check; re-enter execution")
	}
	ref := *c.BundleCheckRef
	if err := validateTypedEvidenceRef(ref, completionBundleCheckEvidenceKind); err != nil {
		return check, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return check, err
	}
	if err := decodeControllerJSON(data, &check); err != nil {
		return check, err
	}
	if err := validateCandidateBundleObservation(c, ref, check, taskID, validations); err != nil {
		return check, err
	}
	if taskID != "" {
		if err := s.verifyCompletionBundleSources(op, c, check); err != nil {
			return check, err
		}
	}
	check.OperationDigest = op.Transition.OperationDigest
	return check, nil
}

func validateCandidateBundleObservation(c AcceptedCandidate, ref EvidenceObjectRef, check CompletionBundleCheck, taskID string, validations []FinalValidationEvidence) error {
	if check.SchemaVersion != controllerSchemaVersion || ref.LogicalIdentity != c.CandidateID || check.FinalOID != c.CommitOID || check.OperationDigest != "" || check.RuntimeTaskID != taskID || !reflect.DeepEqual(check.FinalRuns, validations) {
		return fmt.Errorf("candidate pre-promotion Bundle observation binding differs")
	}
	return nil
}

func (b bundleLifecycleBounds) complete() bool {
	return b.latestTo == string(state.TaskStatusComplete)
}

func (s *Store) completionBundleCheck(op ExecutionOperation, c AcceptedCandidate, runtimeTaskID string, finalValidations []FinalValidationEvidence) (CompletionBundleCheck, error) {
	binding := CompletionBundleCheck{SchemaVersion: controllerSchemaVersion, OperationDigest: op.Transition.OperationDigest, FinalOID: op.Terminal.Record.CandidateOID, RuntimeTaskID: runtimeTaskID}
	if runtimeTaskID == "" {
		binding.TaskClockStatus = evidenceAnalysisStatusAbsent
		binding.TaskClockBasis = completionBundleNoModelRuntimeBasis
		binding.FinalRuns = finalValidations
		return binding, nil
	}
	before, err := s.completionBundleRunStores(op, c)
	if err != nil {
		return binding, err
	}
	check, err := s.observeCompletionBundle(op, c, runtimeTaskID, finalValidations)
	if err != nil {
		return binding, err
	}
	after, err := s.completionBundleRunStores(op, c)
	if err != nil {
		return binding, err
	}
	if err := bindCompletionBundleSources(&check, before, after); err != nil {
		return binding, err
	}
	check.OperationDigest, check.FinalOID = binding.OperationDigest, binding.FinalOID
	return check, nil
}

func (s *Store) observeCompletionBundle(op ExecutionOperation, c AcceptedCandidate, runtimeTaskID string, finalValidations []FinalValidationEvidence) (CompletionBundleCheck, error) {
	export, result, err := s.ExportEvidence(EvidenceExportRequest{TaskID: runtimeTaskID})
	if err != nil {
		return CompletionBundleCheck{}, fmt.Errorf("completion Bundle observation export failed: %w", err)
	}
	if result.Target.RuntimeTaskID != runtimeTaskID || result.Target.AttemptID != op.Terminal.Record.AttemptID || !result.Target.Task.Equal(c.TaskRef) {
		return CompletionBundleCheck{}, fmt.Errorf("completion Bundle observation targets %s attempt %s, not the final attempt %s of the completing Task", result.Target.RuntimeTaskID, result.Target.AttemptID, op.Terminal.Record.AttemptID)
	}
	return verifyFinalBundleObservation(runtimeTaskID, finalValidations, export)
}

func bindCompletionBundleSources(check *CompletionBundleCheck, before, after []CompletionBundleRunStore) error {
	if !reflect.DeepEqual(before, after) {
		return fmt.Errorf("completion Bundle validation run sources changed while the observation was captured: %s became %s", describeBundleRunStores(before), describeBundleRunStores(after))
	}
	check.RunStores = after
	return nil
}

func (s *Store) completionBundleRunStores(op ExecutionOperation, c AcceptedCandidate) ([]CompletionBundleRunStore, error) {
	runtime := state.AttachStateStore(WorkflowTaskConfig(s.config, s.identity, c.TaskRef))
	scans := []bundleRunStoreScan{
		{store: runtime, label: "runtime-task-gates", dir: qualitygate.RunDirectory},
		{store: runtime, label: "runtime-task-smoke", dir: evidenceExportValidationSmokeDirectory},
	}
	workspace, err := s.completionBundleWorkspaceRoot(op)
	if err != nil {
		return nil, err
	}
	if workspace != "" {
		module, err := s.repositoryValidationStore(workspace)
		if err != nil {
			return nil, fmt.Errorf("resolve completion Bundle workspace validation source: %w", err)
		}
		if module != nil {
			scans = append(scans, bundleRunStoreScan{store: module, label: "workspace-gates", dir: qualitygate.RunDirectory})
		}
	}
	stores := make([]CompletionBundleRunStore, 0, len(scans))
	for _, scan := range scans {
		runs, err := completionBundleStoreRuns(scan)
		if err != nil {
			return nil, err
		}
		stores = append(stores, CompletionBundleRunStore{Store: scan.label, Runs: runs})
	}
	return stores, nil
}

func (s *Store) completionBundleWorkspaceRoot(op ExecutionOperation) (string, error) {
	attempt, err := s.loadAttempt(op.Terminal.Record.AttemptID)
	if err != nil {
		return "", err
	}
	head, err := s.LoadHead()
	if err != nil {
		return "", err
	}
	workspace, resolved, err := s.attemptWorkspaceForExport(head, attempt)
	if err != nil {
		return "", fmt.Errorf("resolve completion Bundle validation workspace source: %w", err)
	}
	if !resolved {
		return "", nil
	}
	return workspace.Root, nil
}

func completionBundleStoreRuns(scan bundleRunStoreScan) ([]CompletionBundleRunSource, error) {
	entries, err := os.ReadDir(scan.store.Path(scan.dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []CompletionBundleRunSource{}, nil
		}
		return nil, fmt.Errorf("read completion Bundle validation source %s: %w", scan.label, err)
	}
	runs := []CompletionBundleRunSource{}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		digest, err := completionBundleRunDigest(scan, entry.Name())
		if err != nil {
			return nil, err
		}
		runs = append(runs, CompletionBundleRunSource{RunID: entry.Name(), Digest: digest})
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].RunID < runs[j].RunID })
	return runs, nil
}

func completionBundleRunDigest(scan bundleRunStoreScan, runID string) (string, error) {
	root := scan.store.Path(filepath.Join(scan.dir, runID))
	var digest []byte
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read validation run %s file %s: %w", runID, relative, err)
		}
		digest = append(digest, []byte(filepath.ToSlash(relative)+":"+digestBytes(data)+"\n")...)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("digest completion Bundle validation source %s run %s: %w", scan.label, runID, err)
	}
	return digestBytes(digest), nil
}

func verifyFinalBundleObservation(runtimeTaskID string, finalValidations []FinalValidationEvidence, export EvidenceExport) (CompletionBundleCheck, error) {
	check := CompletionBundleCheck{SchemaVersion: controllerSchemaVersion, RuntimeTaskID: runtimeTaskID, FinalRuns: finalValidations}
	index, err := decodeFinalBundleIndex(export)
	if err != nil {
		return check, err
	}
	lifecycleData, ok := finalBundleTaskFile(export, "lifecycle.jsonl")
	if !ok {
		return check, fmt.Errorf("completion Bundle carries no Task lifecycle evidence for the final Task")
	}
	check.LifecycleDigest = digestBytes(lifecycleData)
	lifecycle, err := parseFinalBundleLifecycle(lifecycleData)
	if err != nil {
		return check, fmt.Errorf("completion Bundle Task lifecycle evidence is unreadable: %w", err)
	}
	if err := verifyFinalBundleTaskClock(index.Workflow.TaskClock, lifecycle, index.GeneratedAt, runtimeTaskID, &check); err != nil {
		return check, err
	}
	telemetry, err := finalBundleTelemetry(export, &check)
	if err != nil {
		return check, err
	}
	if err := verifyFinalBundleModelCalls(index.Workflow.ModelStages, telemetry, runtimeTaskID, &check); err != nil {
		return check, err
	}
	window := &EvidenceAnalysisWindow{Start: index.Workflow.TaskClock.Start, End: index.Workflow.TaskClock.End}
	records, err := parseBundleRunRecords(export)
	if err != nil {
		return check, err
	}
	inventory, err := collectBundleValidationInventory(records, index.CanonicalValidationRuns, runtimeTaskID, window)
	if err != nil {
		return check, err
	}
	if err := verifyBundleAggregateForms(index.Workflow.Validations, inventory, &check); err != nil {
		return check, err
	}
	return check, verifyBundleFinalValidationRuns(inventory, finalValidations, export)
}

func decodeFinalBundleIndex(export EvidenceExport) (EvidenceAnalysisIndex, error) {
	var index EvidenceAnalysisIndex
	indexData, ok := finalBundleFile(export, evidenceAnalysisIndexEntryPath)
	if !ok {
		return index, fmt.Errorf("completion Bundle observation lacks the analysis index")
	}
	if err := decodeControllerJSON(indexData, &index); err != nil {
		return index, fmt.Errorf("completion Bundle analysis index is unreadable: %w", err)
	}
	return index, nil
}

func finalBundleTelemetry(export EvidenceExport, check *CompletionBundleCheck) ([]state.ModelCallLog, error) {
	telemetryData, ok := finalBundleTaskFile(export, "telemetry.jsonl")
	if !ok {
		return nil, fmt.Errorf("completion Bundle carries no worker telemetry evidence for the final Task")
	}
	check.TelemetryDigest = digestBytes(telemetryData)
	telemetry, err := parseFinalBundleTelemetry(telemetryData)
	if err != nil {
		return nil, fmt.Errorf("completion Bundle worker telemetry evidence is unreadable: %w", err)
	}
	return telemetry, nil
}

func verifyFinalBundleTaskClock(clock EvidenceAnalysisTaskClock, lifecycle []state.TaskLifecycleRecord, generatedAt time.Time, runtimeTaskID string, check *CompletionBundleCheck) error {
	bounds, err := scanBundleLifecycleBounds(lifecycle, runtimeTaskID)
	if err != nil {
		return err
	}
	check.TaskClockStatus, check.TaskClockBasis, check.TaskClockStart = clock.Status, clock.Basis, clock.Start
	if clock.Status != evidenceAnalysisStatusCollected {
		return fmt.Errorf("completion Bundle Task clock is %s; the final Task wall clock is unobserved", clock.Status)
	}
	if clock.Basis != workflowTaskClockBasis {
		return fmt.Errorf("completion Bundle Task clock basis %q does not cover the whole Task lifecycle", clock.Basis)
	}
	if !clock.Start.Equal(bounds.start) {
		return fmt.Errorf("completion Bundle Task clock starts at %s; the Task lifecycle starts at %s", clock.Start.Format(time.RFC3339Nano), bounds.start.Format(time.RFC3339Nano))
	}
	return verifyBundleTaskClockEnd(clock, bounds, generatedAt)
}

func scanBundleLifecycleBounds(lifecycle []state.TaskLifecycleRecord, runtimeTaskID string) (bundleLifecycleBounds, error) {
	var bounds bundleLifecycleBounds
	if len(lifecycle) == 0 {
		return bounds, fmt.Errorf("completion Bundle Task lifecycle evidence carries no records")
	}
	bounds.start, bounds.latest = lifecycle[0].Timestamp, lifecycle[0].Timestamp
	bounds.latestTo = lifecycle[0].To
	for _, record := range lifecycle {
		if record.TaskID != runtimeTaskID {
			return bounds, fmt.Errorf("completion Bundle Task lifecycle evidence binds Runtime task %s, not %s", record.TaskID, runtimeTaskID)
		}
		if record.Timestamp.Before(bounds.start) {
			bounds.start = record.Timestamp
		}
		if !record.Timestamp.Before(bounds.latest) {
			bounds.latest = record.Timestamp
			bounds.latestTo = record.To
		}
	}
	if bounds.complete() {
		bounds.completedAt = bounds.latest
	}
	return bounds, nil
}

func verifyBundleTaskClockEnd(clock EvidenceAnalysisTaskClock, bounds bundleLifecycleBounds, generatedAt time.Time) error {
	if clock.End.IsZero() || clock.End.Before(bounds.latest) {
		return fmt.Errorf("completion Bundle Task clock ends at %s, before the latest lifecycle record %s", clock.End.Format(time.RFC3339Nano), bounds.latest.Format(time.RFC3339Nano))
	}
	expectedEnd, expectedBasis := generatedAt, evidenceExportLiveEndBasis
	if bounds.complete() {
		expectedEnd, expectedBasis = bounds.completedAt, workflowTaskClockCompleteBasis
	}
	if clock.EndBasis != expectedBasis {
		return fmt.Errorf("completion Bundle Task clock end basis %q disagrees with the Task lifecycle boundary %q", clock.EndBasis, expectedBasis)
	}
	if !clock.End.Equal(expectedEnd) {
		return fmt.Errorf("completion Bundle Task clock ends at %s while its basis %q observes %s", clock.End.Format(time.RFC3339Nano), clock.EndBasis, expectedEnd.Format(time.RFC3339Nano))
	}
	if wall := clock.End.Sub(clock.Start).Milliseconds(); clock.WallMS != wall {
		return fmt.Errorf("completion Bundle Task clock records %d ms while its window spans %d ms", clock.WallMS, wall)
	}
	return nil
}

func verifyFinalBundleModelCalls(stages []EvidenceAnalysisModelStage, telemetry []state.ModelCallLog, runtimeTaskID string, check *CompletionBundleCheck) error {
	expected, err := bundleTelemetryModelTotals(telemetry, runtimeTaskID)
	if err != nil {
		return err
	}
	observed := map[string]EvidenceAnalysisModelStage{}
	for _, stage := range stages {
		observed[stage.Role] = stage
	}
	if err := compareBundleModelStages(expected, observed); err != nil {
		return err
	}
	if len(expected) > 0 {
		check.ModelCallsByRole = bundleModelCallCounts(expected)
	}
	return nil
}

func bundleTelemetryModelTotals(telemetry []state.ModelCallLog, runtimeTaskID string) (map[string]*bundleModelCallTotals, error) {
	expected := map[string]*bundleModelCallTotals{}
	counted := map[string]bool{}
	for _, call := range telemetry {
		if call.TaskID != runtimeTaskID {
			return nil, fmt.Errorf("completion Bundle worker telemetry binds Runtime task %s, not %s", call.TaskID, runtimeTaskID)
		}
		if call.CallType != state.CallTypeTask && call.CallType != state.CallTypeProbe {
			continue
		}
		if call.CallID != "" && counted[call.CallID] {
			continue
		}
		counted[call.CallID] = true
		observeBundleModelCall(expected, call)
	}
	return expected, nil
}

func observeBundleModelCall(expected map[string]*bundleModelCallTotals, call state.ModelCallLog) {
	totals := expected[string(call.Role)]
	if totals == nil {
		totals = &bundleModelCallTotals{}
		expected[string(call.Role)] = totals
	}
	totals.calls++
	totals.workMS += call.WallDurationMS
	if call.WallDurationMS > totals.slowestMS {
		totals.slowestMS = call.WallDurationMS
	}
	if call.StartedAt.IsZero() || call.CompletedAt.IsZero() || call.CompletedAt.Before(call.StartedAt) {
		totals.unknownDurations++
	} else {
		totals.intervals = append(totals.intervals, workflowInterval{call.StartedAt, call.CompletedAt})
	}
	if call.RetryOf != "" {
		totals.retried++
	}
	if call.Outcome == "interrupted" {
		totals.interrupted++
	}
}

func compareBundleModelStages(expected map[string]*bundleModelCallTotals, observed map[string]EvidenceAnalysisModelStage) error {
	for _, role := range sortedBundleRoles(expected, observed) {
		totals, stage := expected[role], observed[role]
		if totals == nil {
			return fmt.Errorf("completion Bundle model aggregate counts %d calls for role %q without canonical model calls", stage.Calls, role)
		}
		if _, present := observed[role]; !present {
			return fmt.Errorf("completion Bundle model aggregate omits role %q with %d canonical model calls", role, totals.calls)
		}
		if err := compareBundleModelStage(role, stage, totals); err != nil {
			return err
		}
	}
	return nil
}

func compareBundleModelStage(role string, stage EvidenceAnalysisModelStage, totals *bundleModelCallTotals) error {
	switch {
	case stage.Calls != totals.calls:
		return fmt.Errorf("completion Bundle model aggregate counts %d calls for role %q; canonical telemetry holds %d", stage.Calls, role, totals.calls)
	case stage.WorkMS != totals.workMS:
		return fmt.Errorf("completion Bundle model aggregate records %d ms of work for role %q; canonical telemetry holds %d", stage.WorkMS, role, totals.workMS)
	case stage.SlowestMS != totals.slowestMS:
		return fmt.Errorf("completion Bundle model aggregate records slowest call %d ms for role %q; canonical telemetry holds %d", stage.SlowestMS, role, totals.slowestMS)
	case stage.UnknownDurations != totals.unknownDurations:
		return fmt.Errorf("completion Bundle model aggregate records %d unknown durations for role %q; canonical telemetry holds %d", stage.UnknownDurations, role, totals.unknownDurations)
	case stage.Retried != totals.retried:
		return fmt.Errorf("completion Bundle model aggregate records %d retried calls for role %q; canonical telemetry holds %d", stage.Retried, role, totals.retried)
	case stage.Interrupted != totals.interrupted:
		return fmt.Errorf("completion Bundle model aggregate records %d interrupted calls for role %q; canonical telemetry holds %d", stage.Interrupted, role, totals.interrupted)
	case stage.WallMS != workflowIntervalUnionMS(totals.intervals):
		return fmt.Errorf("completion Bundle model aggregate records %d ms wall time for role %q; the known call intervals union to %d ms", stage.WallMS, role, workflowIntervalUnionMS(totals.intervals))
	}
	return nil
}

func bundleModelCallCounts(totals map[string]*bundleModelCallTotals) map[string]int {
	calls := make(map[string]int, len(totals))
	for role, total := range totals {
		calls[role] = total.calls
	}
	return calls
}

func sortedBundleRoles(expected map[string]*bundleModelCallTotals, observed map[string]EvidenceAnalysisModelStage) []string {
	roles := map[string]struct{}{}
	for role := range expected {
		roles[role] = struct{}{}
	}
	for role := range observed {
		roles[role] = struct{}{}
	}
	keys := make([]string, 0, len(roles))
	for role := range roles {
		keys = append(keys, role)
	}
	sort.Strings(keys)
	return keys
}

func parseBundleRunRecords(export EvidenceExport) ([]qualitygate.RunRecord, error) {
	var records []qualitygate.RunRecord
	seen := map[string]bool{}
	for _, file := range export.Files {
		runID, ok := bundleRunRecordPath(file.Path)
		if !ok {
			continue
		}
		if seen[runID] {
			continue
		}
		record, err := qualitygate.Decode(file.Data)
		if err != nil {
			return nil, fmt.Errorf("completion Bundle validation run %s record is unreadable: %w", runID, err)
		}
		if record.ValidationRunID != runID {
			return nil, fmt.Errorf("completion Bundle validation run record at %s identifies itself as %s", file.Path, record.ValidationRunID)
		}
		seen[runID] = true
		records = append(records, record)
	}
	return records, nil
}

func bundleRunRecordPath(path string) (string, bool) {
	dir, name := filepath.Split(path)
	if name != qualitygate.RunFile {
		return "", false
	}
	runDir := strings.TrimSuffix(dir, "/")
	runID := filepath.Base(runDir)
	if !qualitygate.ValidRunID(runID) {
		return "", false
	}
	switch filepath.Base(filepath.Dir(runDir)) {
	case "quality-gate", qualitygate.RunDirectory, "install-smoke":
		return runID, true
	}
	return "", false
}

func collectBundleValidationInventory(records []qualitygate.RunRecord, canonical []EvidenceAnalysisValidationRun, runtimeTaskID string, window *EvidenceAnalysisWindow) (bundleValidationInventory, error) {
	inventory := bundleValidationInventory{
		byForm:  map[string]*bundleFormAggregate{},
		byRunID: map[string]EvidenceAnalysisValidationRun{},
	}
	for _, run := range canonical {
		if run.RunID == "" {
			continue
		}
		if _, duplicate := inventory.byRunID[run.RunID]; !duplicate {
			inventory.byRunID[run.RunID] = run
		}
	}
	for _, record := range records {
		entry, ok := inventory.byRunID[record.ValidationRunID]
		if !ok {
			return inventory, fmt.Errorf("completion Bundle validation run %s is absent from the canonical run inventory", record.ValidationRunID)
		}
		if entry.Form == "" || record.Form == "" {
			continue
		}
		if !bundleRunInTaskScope(record, entry.Binding, runtimeTaskID, window) {
			continue
		}
		totals := inventory.byForm[record.Form]
		if totals == nil {
			totals = &bundleFormAggregate{}
			inventory.byForm[record.Form] = totals
		}
		observeBundleFormRun(totals, record)
	}
	return inventory, nil
}

func bundleRunInTaskScope(record qualitygate.RunRecord, binding, runtimeTaskID string, window *EvidenceAnalysisWindow) bool {
	if record.TaskID != "" {
		return record.TaskID == runtimeTaskID
	}
	return binding == evidenceValidationBindingTaskStore || workflowRunContained(record, window)
}

func observeBundleFormRun(totals *bundleFormAggregate, record qualitygate.RunRecord) {
	totals.runs++
	switch record.Status {
	case qualitygate.StatusPass:
		totals.pass++
	case qualitygate.StatusFail:
		totals.fail++
	case qualitygate.StatusInterrupted:
		totals.interrupted++
	}
	if workflowValidationDurationKnown(record) {
		totals.durationMS += record.DurationMS
		totals.intervals = append(totals.intervals, workflowInterval{record.StartedAt, *record.CompletedAt})
	} else {
		totals.unknownDuration++
	}
}

func verifyBundleAggregateForms(published []EvidenceAnalysisValidationSummary, inventory bundleValidationInventory, check *CompletionBundleCheck) error {
	for _, summary := range published {
		totals, ok := inventory.byForm[summary.Form]
		if !ok {
			return fmt.Errorf("completion Bundle workflow aggregate counts form %s the Task does not own", summary.Form)
		}
		if err := compareBundleFormAggregate(summary, totals); err != nil {
			return err
		}
		delete(inventory.byForm, summary.Form)
		check.Validations = append(check.Validations, CompletionBundleValidationView{
			Form: summary.Form, Runs: summary.Runs, Pass: summary.Pass, Fail: summary.Fail,
			Interrupted: summary.Interrupted, DurationMS: summary.DurationMS, WallMS: summary.WallMS,
			UnknownDuration: summary.UnknownDurations,
		})
	}
	if len(inventory.byForm) != 0 {
		forms := make([]string, 0, len(inventory.byForm))
		for form := range inventory.byForm {
			forms = append(forms, form)
		}
		sort.Strings(forms)
		return fmt.Errorf("completion Bundle workflow aggregate omits form %s with %d Task-scoped canonical runs", forms[0], inventory.byForm[forms[0]].runs)
	}
	return nil
}

func compareBundleFormAggregate(summary EvidenceAnalysisValidationSummary, totals *bundleFormAggregate) error {
	switch {
	case summary.Runs != totals.runs:
		return fmt.Errorf("completion Bundle workflow aggregate counts %d %s runs; the Task owns %d", summary.Runs, summary.Form, totals.runs)
	case summary.Pass != totals.pass:
		return fmt.Errorf("completion Bundle workflow aggregate counts %d passing %s runs; the Task owns %d", summary.Pass, summary.Form, totals.pass)
	case summary.Fail != totals.fail:
		return fmt.Errorf("completion Bundle workflow aggregate counts %d failing %s runs; the Task owns %d", summary.Fail, summary.Form, totals.fail)
	case summary.Interrupted != totals.interrupted:
		return fmt.Errorf("completion Bundle workflow aggregate counts %d interrupted %s runs; the Task owns %d", summary.Interrupted, summary.Form, totals.interrupted)
	case summary.DurationMS != totals.durationMS:
		return fmt.Errorf("completion Bundle workflow aggregate records %d ms of %s run time; the Task owns %d", summary.DurationMS, summary.Form, totals.durationMS)
	case summary.UnknownDurations != totals.unknownDuration:
		return fmt.Errorf("completion Bundle workflow aggregate records %d unknown-duration %s runs; the Task owns %d", summary.UnknownDurations, summary.Form, totals.unknownDuration)
	case summary.WallMS != workflowIntervalUnionMS(totals.intervals):
		return fmt.Errorf("completion Bundle workflow aggregate records %d ms wall time for %s; the known run intervals union to %d ms", summary.WallMS, summary.Form, workflowIntervalUnionMS(totals.intervals))
	}
	return nil
}

func verifyBundleFinalValidationRuns(inventory bundleValidationInventory, finalValidations []FinalValidationEvidence, export EvidenceExport) error {
	for _, finalRun := range finalValidations {
		run, ok := inventory.byRunID[finalRun.ValidationRunID]
		if !ok {
			return fmt.Errorf("completion Bundle lacks canonical validation run %s for the final %s gate", finalRun.ValidationRunID, finalRun.Form)
		}
		if run.Form != finalRun.Form {
			return fmt.Errorf("completion Bundle validation run %s records form %s, not %s", finalRun.ValidationRunID, run.Form, finalRun.Form)
		}
		if run.Result != qualitygate.StatusPass {
			return fmt.Errorf("completion Bundle validation run %s records result %s, not a terminal pass", finalRun.ValidationRunID, run.Result)
		}
		if err := verifyBundleRunTimings(finalRun.ValidationRunID, finalRun.Form, run.TestTimings, export); err != nil {
			return err
		}
	}
	return nil
}

func verifyBundleRunTimings(runID, form, locator string, export EvidenceExport) error {
	if locator == "" {
		return fmt.Errorf("completion Bundle validation run %s carries no test timing evidence", runID)
	}
	data, ok := finalBundleFile(export, locator)
	if !ok {
		return fmt.Errorf("completion Bundle lacks the test timing evidence entry %s for validation run %s", locator, runID)
	}
	timings, err := qualitygate.DecodeTestTimings(data)
	if err != nil {
		return fmt.Errorf("completion Bundle test timing evidence for validation run %s is unreadable: %w", runID, err)
	}
	if timings.ValidationRunID != runID || timings.Form != form {
		return fmt.Errorf("completion Bundle test timing evidence for validation run %s identifies itself as %s (%s)", runID, timings.ValidationRunID, timings.Form)
	}
	if !timings.Coverage.Captured {
		return fmt.Errorf("completion Bundle test timing evidence for validation run %s was not captured from machine-readable test events", runID)
	}
	return nil
}

func (s *Store) loadCompletionBundleCheck(op ExecutionOperation, ready CompletionReadiness) (CompletionBundleCheck, error) {
	var check CompletionBundleCheck
	if ready.BundleCheckRef == nil {
		return check, fmt.Errorf("completion readiness lacks the final Bundle observation check")
	}
	ref := *ready.BundleCheckRef
	if err := validateTypedEvidenceRef(ref, completionBundleCheckEvidenceKind); err != nil {
		return check, err
	}
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return check, err
	}
	if err := decodeControllerJSON(data, &check); err != nil {
		return check, err
	}
	if check.SchemaVersion != controllerSchemaVersion || ref.LogicalIdentity != op.Transition.TransitionID || check.OperationDigest != op.Transition.OperationDigest || check.FinalOID != op.Terminal.Record.CandidateOID {
		return check, fmt.Errorf("completion Bundle observation check belongs to a different operation")
	}
	if check.RuntimeTaskID != ready.RuntimeTaskID {
		return check, fmt.Errorf("completion Bundle observation check binds Runtime task %s, not %s", check.RuntimeTaskID, ready.RuntimeTaskID)
	}
	if check.RuntimeTaskID != "" && check.RunStores == nil {
		return check, fmt.Errorf("completion Bundle observation check lacks its validation source bindings")
	}
	return check, nil
}

func (s *Store) verifyCompletionBundleCheck(op ExecutionOperation, c AcceptedCandidate, ready CompletionReadiness) error {
	saved, err := s.loadCompletionBundleCheck(op, ready)
	if err != nil {
		return err
	}
	if saved.RuntimeTaskID == "" {
		return nil
	}
	return s.verifyCompletionBundleSources(op, c, saved)
}

func (s *Store) verifyCompletionBundleSources(op ExecutionOperation, c AcceptedCandidate, saved CompletionBundleCheck) error {
	runtime := state.AttachStateStore(WorkflowTaskConfig(s.config, s.identity, c.TaskRef))
	lifecycle, err := os.ReadFile(runtime.TaskLifecycleLogPath(saved.RuntimeTaskID))
	if err != nil {
		return fmt.Errorf("completion Bundle observation Task lifecycle source became unreadable: %w", err)
	}
	if digestBytes(lifecycle) != saved.LifecycleDigest {
		return fmt.Errorf("completion Bundle observation Task lifecycle evidence changed after readiness")
	}
	telemetry, err := os.ReadFile(runtime.ModelCallLogPath(saved.RuntimeTaskID))
	if err != nil {
		return fmt.Errorf("completion Bundle observation worker telemetry source became unreadable: %w", err)
	}
	if digestBytes(telemetry) != saved.TelemetryDigest {
		return fmt.Errorf("completion Bundle observation worker telemetry evidence changed after readiness")
	}
	stores, err := s.completionBundleRunStores(op, c)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(stores, saved.RunStores) {
		return fmt.Errorf("completion Bundle observation validation run sources changed after readiness: saved %s, observed %s", describeBundleRunStores(saved.RunStores), describeBundleRunStores(stores))
	}
	return nil
}

func describeBundleRunStores(stores []CompletionBundleRunStore) string {
	parts := make([]string, 0, len(stores))
	for _, store := range stores {
		parts = append(parts, fmt.Sprintf("%s:%d runs", store.Store, len(store.Runs)))
	}
	return strings.Join(parts, ", ")
}

func finalBundleFile(export EvidenceExport, path string) ([]byte, bool) {
	for _, file := range export.Files {
		if file.Path == path {
			return file.Data, true
		}
	}
	return nil, false
}

func finalBundleTaskFile(export EvidenceExport, name string) ([]byte, bool) {
	for _, path := range []string{"live/task/" + name, "bound/task/" + name, "attempt/" + name} {
		if data, ok := finalBundleFile(export, path); ok {
			return data, true
		}
	}
	return nil, false
}

func parseFinalBundleLifecycle(data []byte) ([]state.TaskLifecycleRecord, error) {
	var records []state.TaskLifecycleRecord
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		record, err := state.ParseTaskLifecycleLine([]byte(line))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func parseFinalBundleTelemetry(data []byte) ([]state.ModelCallLog, error) {
	var logs []state.ModelCallLog
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var log state.ModelCallLog
		if err := json.Unmarshal([]byte(line), &log); err != nil {
			return nil, err
		}
		if log.CallID == "" || log.Version != state.ModelCallLogVersion {
			return nil, fmt.Errorf("model call log record is not a current telemetry record")
		}
		logs = append(logs, log)
	}
	return logs, nil
}
