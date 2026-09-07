package app

import (
	"bytes"
	"encoding/json"
	"errors"

	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentEvidenceFixture struct {
	repoRoot string
	cfg      config.AppConfig
	st       *state.StateStore
}

type parentEvidenceResult struct {
	Output parentEvidenceOutput
	Raw    string
}

func TestPrintParentEvidenceBatchesIndependentReadsIntoOneOwnerCall(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	fixture := newParentEvidenceFixture(t)

	first := runParentEvidence(t, fixture, parentEvidenceManifest{
		Version: 1,
		Reason:  "first anchor",
		Authority: []parentEvidenceAuthorityRequest{
			{Kind: "rules", BudgetBytes: 4096},
			{Kind: "plan", BudgetBytes: 4096},
			{Kind: "active", BudgetBytes: 4096},
		},
		Handoff:     &parentEvidenceHandoffRequest{},
		Status:      &parentEvidenceStatusRequest{},
		Validations: &parentEvidenceValidationsRequest{},
		Telemetry:   &parentEvidenceTelemetryRequest{},
		Search: []parentEvidenceSearchRequest{
			{Question: "park lifecycle owner", Scopes: []string{"docs"}, BudgetBytes: 4096},
		},
		Diff: []parentEvidenceDiffRequest{
			{Question: "what changed in tracked file", Paths: []string{"tracked.md"}, BudgetBytes: 4096},
		},
		Source: []parentEvidenceSourceRequest{
			{Question: "exact retention rule text", Path: "docs/guide.md", LineStart: 1, LineEnd: 3, BudgetBytes: 4096},
		},
	})
	if first.Output.Status != parentEvidenceStatusOK {
		t.Fatalf("first output status = %q parts = %s", first.Output.Status, first.Raw)
	}
	if len(first.Output.Parts) != 10 {
		t.Fatalf("parts = %d, want 10: %s", len(first.Output.Parts), first.Raw)
	}
	authorityParts := first.Output.Parts[0:3]
	for _, part := range authorityParts {
		if part.Status != parentEvidencePartProjected || part.Authority == nil || part.Authority.Content == "" {
			t.Fatalf("first authority part = %#v", part)
		}
	}
	handoffPart := first.Output.Parts[3]
	if handoffPart.Status != parentEvidencePartProjected || len(handoffPart.Handoff) == 0 || handoffPart.Digest == "" {
		t.Fatalf("handoff part = %#v", handoffPart)
	}
	searchPart := first.Output.Parts[7]
	if searchPart.Search == nil || searchPart.Search.ResultCount == 0 || len(searchPart.Search.Results) == 0 {
		t.Fatalf("search part = %#v", searchPart)
	}
	if searchPart.Search.Results[0].Path != "docs/guide.md" {
		t.Fatalf("search results = %#v", searchPart.Search.Results)
	}
	diffPart := first.Output.Parts[8]
	if diffPart.Diff == nil || len(diffPart.Diff.Files) != 1 || diffPart.Diff.Files[0].WorktreeSHA == "" {
		t.Fatalf("diff part = %#v", diffPart.Diff)
	}
	if diffPart.Diff.Files[0].HeadBlob == "" || !strings.Contains(diffPart.Diff.Body, "tracked.md") || !strings.Contains(diffPart.Diff.Body, "modified") {
		t.Fatalf("diff identity missing: %#v body = %q", diffPart.Diff.Files[0], diffPart.Diff.Body)
	}
	sourcePart := first.Output.Parts[9]
	if sourcePart.Source == nil {
		t.Fatalf("source part = %#v", sourcePart.Source)
	}
	content := sourcePart.Source.Content
	if !strings.HasPrefix(content, "alpha ") || !strings.HasSuffix(content, "beta\n") || strings.Count(content, "\n") != 2 {
		t.Fatalf("source content = %q", content)
	}
	if sourcePart.Locator != "docs/guide.md:1-3" {
		t.Fatalf("source locator = %q", sourcePart.Locator)
	}

	var stdout bytes.Buffer
	err := printParentHandoffLeased(fixture.st, &stdout)
	var duplicate *DuplicateParentProjectionError
	if !errors.As(err, &duplicate) {
		t.Fatalf("standalone handoff after evidence batch = %v stdout = %s", err, stdout.String())
	}

	second := runParentEvidence(t, fixture, parentEvidenceManifest{
		Version: 1,
		Reason:  "re-anchor with known digests",
		Authority: []parentEvidenceAuthorityRequest{
			{Kind: "rules", KnownContentSHA256: authorityParts[0].Digest, BudgetBytes: 4096},
			{Kind: "plan", KnownContentSHA256: authorityParts[1].Digest, BudgetBytes: 4096},
			{Kind: "active", KnownContentSHA256: authorityParts[2].Digest, BudgetBytes: 4096},
		},
		Handoff: &parentEvidenceHandoffRequest{KnownDigest: handoffPart.Digest},
	})
	if second.Output.Status != parentEvidenceStatusOK {
		t.Fatalf("second output status = %q parts = %s", second.Output.Status, second.Raw)
	}
	for index := 0; index < 3; index++ {
		part := second.Output.Parts[index]
		if part.Status != parentEvidencePartUnchanged || part.Authority == nil || part.Authority.Content != "" {
			t.Fatalf("unchanged authority part = %#v", part)
		}
		if len(part.Authority.Content) != 0 || part.Bytes != len(part.Digest) {
			t.Fatalf("unchanged authority part still carries body: %#v", part)
		}
	}
	if second.Output.Parts[3].Status != parentEvidencePartUnchanged || len(second.Output.Parts[3].Handoff) != 0 {
		t.Fatalf("unchanged handoff part = %#v", second.Output.Parts[3])
	}
	if bytes.Contains([]byte(second.Raw), []byte("authority bootstrap")) {
		t.Fatalf("unchanged projection re-emits known body: %s", second.Raw)
	}
}

func TestPrintParentEvidenceReturnsRefinementInsteadOfTruncatedBodies(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	fixture := newParentEvidenceFixture(t)
	result := runParentEvidence(t, fixture, parentEvidenceManifest{
		Version: 1,
		Reason:  "budgets force refinement",
		Authority: []parentEvidenceAuthorityRequest{
			{Kind: "active", BudgetBytes: 2},
		},
		Search: []parentEvidenceSearchRequest{
			{Question: "park lifecycle owner", Scopes: []string{"docs"}, BudgetBytes: 2},
		},
		Diff: []parentEvidenceDiffRequest{
			{Question: "tracked change", Paths: []string{"tracked.md"}, BudgetBytes: 2},
		},
		Source: []parentEvidenceSourceRequest{
			{Question: "guide head", Path: "docs/guide.md", LineStart: 1, LineEnd: 3, BudgetBytes: 2},
		},
	})
	if result.Output.Status != parentEvidenceStatusRequired {
		t.Fatalf("status = %q, want refinement_required: %s", result.Output.Status, result.Raw)
	}
	for _, part := range result.Output.Parts {
		if part.Status != parentEvidencePartRefinement {
			t.Fatalf("part %s status = %q, want refinement_required", part.Kind, part.Status)
		}
		if part.Reason == "" {
			t.Fatalf("refinement part without reason: %#v", part)
		}
	}
	if result.Output.Parts[0].Authority.Content != "" {
		t.Fatalf("refined authority part carries body: %#v", result.Output.Parts[0].Authority)
	}
	if len(result.Output.Parts[2].Diff.Files) != 1 || result.Output.Parts[2].Diff.Files[0].WorktreeSHA == "" {
		t.Fatalf("refined diff part loses identity: %#v", result.Output.Parts[2].Diff)
	}
	if result.Output.Parts[3].Source.Content != "" || result.Output.Parts[3].Locator != "docs/guide.md:1-3" {
		t.Fatalf("refined source part = %#v", result.Output.Parts[3].Source)
	}
}

func TestPrintParentEvidenceRejectsMalformedManifests(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	manifestPath := filepath.Join(fixture.repoRoot, "manifest.json")

	if err := os.WriteFile(manifestPath, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadParentEvidenceManifest(manifestPath); err == nil {
		t.Fatal("manifest without parts or reason was accepted")
	}

	if err := os.WriteFile(manifestPath, []byte(`{"version":2,"reason":"x","handoff":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var usage *UsageError
	if _, err := loadParentEvidenceManifest(manifestPath); !errors.As(err, &usage) {
		t.Fatalf("version 2 manifest error = %T, want UsageError", err)
	}

	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"reason":"x","unknown":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadParentEvidenceManifest(manifestPath); !errors.As(err, &usage) {
		t.Fatalf("unknown field manifest error = %T, want UsageError", err)
	}

	var notFound *NotFoundError
	if _, err := loadParentEvidenceManifest(filepath.Join(fixture.repoRoot, "missing.json")); !errors.As(err, &notFound) {
		t.Fatalf("missing manifest error = %T, want NotFoundError", err)
	}
}

func TestExecuteParentEvidenceFromArgv(t *testing.T) {
	if _, err := execLookPathGit(); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	fixture := newParentEvidenceFixture(t)
	modelCalls := &fakeRunner{}
	executeManifest := func(manifestPath string) (parentEvidenceOutput, string) {
		t.Helper()
		command, err := ParseCommand([]string{"--evidence", manifestPath})
		if err != nil {
			t.Fatalf("ParseCommand(--evidence %s): %v", manifestPath, err)
		}
		var stdout bytes.Buffer
		if err := Execute(command, fixture.cfg, modelCalls.factory(), &stdout, io.Discard); err != nil {
			t.Fatalf("Execute(--evidence %s): %v stdout=%s", manifestPath, err, stdout.String())
		}
		var output parentEvidenceOutput
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			t.Fatalf("evidence出力がmachine JSONではありません: %v: %s", err, stdout.String())
		}
		return output, stdout.String()
	}
	writeManifest := func(body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "evidence-manifest.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	manifestBody := func(request parentEvidenceManifest) string {
		t.Helper()
		data, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	if _, err := ParseCommand([]string{"--evidence"}); err == nil {
		t.Fatal("manifest pathなしの--evidenceはusage errorである必要があります")
	}

	first, firstRaw := executeManifest(writeManifest(manifestBody(parentEvidenceManifest{
		Version:   1,
		Reason:    "argv entrance",
		Authority: []parentEvidenceAuthorityRequest{{Kind: "active", BudgetBytes: 4096}},
		Status:    &parentEvidenceStatusRequest{},
	})))
	if first.Status != parentEvidenceStatusOK {
		t.Fatalf("argv入口のstatus = %q raw=%s", first.Status, firstRaw)
	}
	if len(first.Parts) != 2 || first.Parts[0].Authority == nil || first.Parts[0].Authority.Content == "" {
		t.Fatalf("argv入口のauthority part = %#v raw=%s", first.Parts, firstRaw)
	}
	if len(first.Parts[1].StatusRead) == 0 {
		t.Fatalf("argv入口のstatus part = %#v raw=%s", first.Parts[1], firstRaw)
	}

	known, knownRaw := executeManifest(writeManifest(manifestBody(parentEvidenceManifest{
		Version: 1,
		Reason:  "known digest re-fetch",
		Authority: []parentEvidenceAuthorityRequest{
			{Kind: "active", KnownContentSHA256: first.Parts[0].Digest, BudgetBytes: 4096},
		},
	})))
	knownPart := known.Parts[0]
	if knownPart.Status != parentEvidencePartUnchanged || knownPart.Authority == nil || knownPart.Authority.Content != "" {
		t.Fatalf("既知digest再取得 = %#v raw=%s", knownPart, knownRaw)
	}
	if strings.Contains(knownRaw, "active task body") {
		t.Fatalf("既知digest再取得が既知本文を再出力しました: %s", knownRaw)
	}

	refined, refinedRaw := executeManifest(writeManifest(manifestBody(parentEvidenceManifest{
		Version:   1,
		Reason:    "budget overrun",
		Authority: []parentEvidenceAuthorityRequest{{Kind: "active", BudgetBytes: 1}},
	})))
	if refined.Status != parentEvidenceStatusRequired || refined.Parts[0].Status != parentEvidencePartRefinement || refined.Parts[0].Reason == "" {
		t.Fatalf("budget超過 = %q %#v raw=%s", refined.Status, refined.Parts[0], refinedRaw)
	}
	if refined.Parts[0].Authority != nil && refined.Parts[0].Authority.Content != "" {
		t.Fatalf("budget超過partが本文を持ちます: %#v raw=%s", refined.Parts[0].Authority, refinedRaw)
	}

	var malformedOut bytes.Buffer
	malformed, err := ParseCommand([]string{"--evidence", writeManifest(`{"version":1,"reason":"x","typo":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(malformed, fixture.cfg, modelCalls.factory(), &malformedOut, io.Discard); err == nil {
		t.Fatalf("malformed manifestが受理されました: %s", malformedOut.String())
	}
	if malformedOut.Len() != 0 {
		t.Fatalf("malformed manifestでstdoutへ出力しました: %s", malformedOut.String())
	}

	if len(modelCalls.prompts) != 0 || len(modelCalls.models) != 0 {
		t.Fatalf("evidence batchがmodelを呼び出しました: prompts=%d models=%d", len(modelCalls.prompts), len(modelCalls.models))
	}
}

func TestPrintParentEvidenceRecordsTelemetrySummary(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	fixture := newParentEvidenceFixture(t)
	runParentEvidence(t, fixture, parentEvidenceManifest{
		Version:   1,
		Reason:    "telemetry coverage",
		Telemetry: &parentEvidenceTelemetryRequest{},
	})
	records, err := fixture.st.ReadParentEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("parent evidence telemetry was not recorded")
	}
	summary := state.SummarizeParentEvidence(records)
	if summary.OwnerCalls != 1 {
		t.Fatalf("owner calls = %d, want 1", summary.OwnerCalls)
	}
}

func runParentEvidence(t *testing.T, fixture *parentEvidenceFixture, manifest parentEvidenceManifest) parentEvidenceResult {
	t.Helper()
	manifestPath := filepath.Join(t.TempDir(), "evidence-manifest.json")
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(manifestPath) })
	var stdout bytes.Buffer
	if err := printParentEvidence(Command{Mode: ModeEvidence, EvidenceManifest: manifestPath}, fixture.cfg, fixture.st, &stdout); err != nil {
		t.Fatalf("printParentEvidence: %v", err)
	}
	var output parentEvidenceOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("evidence output is not valid JSON: %v: %s", err, stdout.String())
	}
	return parentEvidenceResult{Output: output, Raw: stdout.String()}
}

func newParentEvidenceFixture(t *testing.T) *parentEvidenceFixture {
	t.Helper()
	t.Setenv("GLM_WORKER_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v失敗: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.email", "evidence@example.invalid")
	run("config", "user.name", "evidence test")
	write := func(rel string, content string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("IMPLEMENTATION_RULES.md", "rules body\n")
	write("IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/current.md`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/next.md`\n")
	write("IMPLEMENTATION_TASKS/current.md", "active task body\n")
	write("docs/guide.md", "alpha park lifecycle owner\nbeta\n")
	write("tracked.md", "park lifecycle owner note\n")
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	write("tracked.md", "modified park lifecycle owner note\n")
	write("extra-untracked.md", "untracked body\n")

	cfg := config.AppConfig{
		StateBase:  filepath.Join(t.TempDir(), "state"),
		RepoHash:   "evidencetest",
		RepoRoot:   dir,
		RepoSearch: true,
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("repo-root", dir); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	return &parentEvidenceFixture{repoRoot: dir, cfg: cfg, st: st}
}

func TestParentEvidenceHandoffPartCarriesSessionRotation(t *testing.T) {
	cfg, st, threadID := seedSessionRotationAccept(t)
	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil || marker == nil || marker.Directive == nil {
		t.Fatalf("marker = %#v err=%v", marker, err)
	}
	manifestPath := filepath.Join(t.TempDir(), "evidence-manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"reason":"rotation","handoff":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := printParentEvidence(Command{Mode: ModeEvidence, EvidenceManifest: manifestPath}, cfg, st, &stdout); err != nil {
		t.Fatalf("printParentEvidence: %v", err)
	}
	var output parentEvidenceOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("evidence output is not valid JSON: %v: %s", err, stdout.String())
	}
	if len(output.Parts) != 1 || output.Parts[0].Handoff == nil {
		t.Fatalf("handoff part = %#v raw=%s", output.Parts, stdout.String())
	}
	var handoff parentHandoffOutput
	if err := json.Unmarshal(output.Parts[0].Handoff, &handoff); err != nil {
		t.Fatalf("handoff part JSON: %v", err)
	}
	if handoff.SessionRotation == nil || handoff.SessionRotation.State != state.SessionRotationProjectionPending {
		t.Fatalf("evidence handoff session_rotation = %#v", handoff.SessionRotation)
	}
	if handoff.SessionRotation.Directive == nil || handoff.SessionRotation.Directive.DirectiveID != marker.Directive.DirectiveID {
		t.Fatalf("evidence handoff directive = %#v want %s", handoff.SessionRotation.Directive, marker.Directive.DirectiveID)
	}
}
