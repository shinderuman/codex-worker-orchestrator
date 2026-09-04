package app

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const installSmokeAssertionMarker = "assertion failed: install smoke fixture"

func TestInstallSmokeFailureSharesEvidenceLocatorBetweenRecordAndError(t *testing.T) {
	cfg, st, failFlagPath, countPath := newInstallSmokeEnv(t)
	if err := os.WriteFile(failFlagPath, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err = runInstallSmoke("parent", cfg, st, &stdout)
	var smokeFail *InstallSmokeError
	if !errors.As(err, &smokeFail) {
		t.Fatalf("install smoke失敗errorがInstallSmokeErrorではありません: %v", err)
	}
	if smokeFail.ExitSource != state.ValidationExitSourceTarget || smokeFail.ExitCode != 1 {
		t.Fatalf("exit = %d/%s", smokeFail.ExitCode, smokeFail.ExitSource)
	}
	if smokeFail.EvidenceWarning != "" || smokeFail.Truncated {
		t.Fatalf("evidence warning = %q truncated = %v", smokeFail.EvidenceWarning, smokeFail.Truncated)
	}
	record := readSingleValidationEvent(t, st, taskID)
	if record.Validation.Result != "fail" || record.Validation.Scope != "parent" ||
		record.Validation.Attribution != "task" || record.Validation.ExitSource != state.ValidationExitSourceTarget {
		t.Fatalf("validation = %#v", record.Validation)
	}
	if record.Validation.Evidence == "" || record.Validation.Evidence != smokeFail.Evidence {
		t.Fatalf("validation evidence = %q want %q", record.Validation.Evidence, smokeFail.Evidence)
	}
	detail := installSmokeFailDetail(smokeFail)
	if detail["evidence"] != smokeFail.Evidence {
		t.Fatalf("process error evidence = %v want %q", detail["evidence"], smokeFail.Evidence)
	}
	data, err := os.ReadFile(smokeFail.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, installSmokeAssertionMarker) || !strings.Contains(text, "install smoke stdout fixture") {
		t.Fatalf("evidenceに失敗時の出力が含まれません: %q", text)
	}
	if strings.Contains(text, "fixture-secret-value") || !strings.Contains(text, "SMOKE_TOKEN=[redacted]") {
		t.Fatalf("evidenceのsecretがsanitizationされていません: %q", text)
	}
	if strings.Contains(text, "sk-proj-AbC12345xY") || !strings.Contains(text, "rejected [redacted] during install") {
		t.Fatalf("evidenceのmixed-case tokenがsanitizationされていません: %q", text)
	}
	if strings.Contains(text, "OpenSesame") ||
		!strings.Contains(text, "https://[redacted]@github.example.invalid/org/repo.git") {
		t.Fatalf("evidenceのURL userinfo credentialがsanitizationされていません: %q", text)
	}
	if strings.Contains(text, "ghp_AbC1defGHI456jklMNO789pqrSTU") || strings.Contains(text, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("evidenceのstandalone credential tokenがsanitizationされていません: %q", text)
	}
	if !strings.Contains(text, "kept https://github.example.invalid/org/repo.git and uuid 3f2504e0-4f89-11d3-9a0c-0305e82c3301") {
		t.Fatalf("evidenceのcredentialでないURLとuuidが過剰にredactされています: %q", text)
	}
	if count := smokeInvocationCount(t, countPath); count != 1 {
		t.Fatalf("install smoke実行回数 = %d want 1", count)
	}
}

func TestInstallSmokeFailureEvidenceIdentifiesAssertionWithoutRerun(t *testing.T) {
	cfg, _, failFlagPath, countPath := newInstallSmokeEnv(t)
	if err := os.WriteFile(failFlagPath, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run(
		[]string{"--install-smoke", "--role", "parent"},
		func() (config.AppConfig, error) { return cfg, nil },
		nil,
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	var smokeFail *InstallSmokeError
	if !errors.As(err, &smokeFail) {
		t.Fatalf("install smoke失敗errorがInstallSmokeErrorではありません: %v", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("machine streamsへ出力が漏れています: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	body := buildProcessError(err)
	if body.Kind != errorKindInstallSmokeFailed {
		t.Fatalf("process error kind = %q", body.Kind)
	}
	evidence, _ := body.Detail["evidence"].(string)
	if evidence == "" || evidence != smokeFail.Evidence {
		t.Fatalf("process error evidence = %q want %q", evidence, smokeFail.Evidence)
	}
	standalone := readSingleStandaloneValidation(t, state.AttachStateStore(cfg))
	if standalone.Validation.Result != "fail" || standalone.Validation.Scope != "parent" ||
		standalone.Validation.Attribution != "standalone" || standalone.Validation.Evidence != evidence {
		t.Fatalf("standalone validation = %#v", standalone.Validation)
	}
	data, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), installSmokeAssertionMarker) {
		t.Fatalf("evidenceから失敗assertionを特定できません: %q", string(data))
	}
	if count := smokeInvocationCount(t, countPath); count != 1 {
		t.Fatalf("install smoke実行回数 = %d want 1", count)
	}
}

func TestInstallSmokeEvidenceBoundsAndSanitizesUnsafeOutput(t *testing.T) {
	script := "#!/bin/sh\nset -eu\n" +
		"i=0\n" +
		"while [ \"$i\" -lt 900 ]; do\n" +
		"printf '%s\\n' \"$i 0123456789abcdef0123456789abcdef0123456789\"\n" +
		"i=$((i+1))\n" +
		"done\n" +
		"printf 'bad-bytes:\\377\\376 nul:\\000\\n'\n" +
		"printf '%s\\n' 'assertion failed: bounded tail marker'\n" +
		"exit 1\n"
	cfg, st := newInstallSmokeScriptEnv(t, script)

	var stdout bytes.Buffer
	err := runInstallSmoke("worker", cfg, st, &stdout)
	var smokeFail *InstallSmokeError
	if !errors.As(err, &smokeFail) {
		t.Fatalf("install smoke失敗errorがInstallSmokeErrorではありません: %v", err)
	}
	if !smokeFail.Truncated {
		t.Fatal("上限超過出力がtruncatedとして扱われていません")
	}
	if detail := installSmokeFailDetail(smokeFail); detail["truncated"] != true {
		t.Fatalf("process error detail = %#v", detail)
	}
	data, err := os.ReadFile(smokeFail.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > installSmokeEvidenceLimit {
		t.Fatalf("evidence size = %d want <= %d", len(data), installSmokeEvidenceLimit)
	}
	text := string(data)
	if !utf8.ValidString(text) {
		t.Fatalf("evidenceがvalid UTF-8ではありません: %q", text)
	}
	if !strings.Contains(text, "assertion failed: bounded tail marker") || strings.HasPrefix(text, "0 ") {
		t.Fatalf("evidenceが末尾側を保持していません: %q", text[:80])
	}
	if !strings.Contains(text, "bad-bytes:� nul:�") {
		t.Fatalf("binary出力が安全に置換されていません: %q", text[len(text)-120:])
	}
}

func TestInstallSmokeEvidenceDropsSecretStraddlingCaptureBoundary(t *testing.T) {
	_, st := newInstallSmokeScriptEnv(t, "#!/bin/sh\nexit 1\n")
	secret := "ghp_" + strings.Repeat("Boundary9", 4)
	fragment := secret[len(secret)-24:]
	marker := "assertion failed: capture boundary marker"
	remaining := installSmokeEvidenceLimit - len(fragment)
	filler := strings.Repeat("q", remaining-len(marker)-3) + "\n"
	output := []byte("straddle " + secret + "\n" + filler + marker + "\n")

	var capture installSmokeCapture
	for len(output) > 0 {
		end := min(7000, len(output))
		if _, err := capture.Write(output[:end]); err != nil {
			t.Fatal(err)
		}
		output = output[end:]
	}

	path, warning := saveInstallSmokeEvidence(st, &capture, nil, state.ValidationExitSourceTarget)
	if warning != "" {
		t.Fatalf("evidence warning = %q", warning)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if len(data) > installSmokeEvidenceLimit {
		t.Fatalf("evidence size = %d want <= %d", len(data), installSmokeEvidenceLimit)
	}
	if strings.Contains(text, secret) || strings.Contains(text, fragment) || strings.Contains(text, "straddle") {
		t.Fatalf("capture境界をまたいだsecretがevidenceへrawで残っています: %q", text[:80])
	}
	if !strings.Contains(text, marker) {
		t.Fatalf("evidenceが境界行以降を保持していません: %q", text[:80])
	}

	var newlineless installSmokeCapture
	blobSecret := "ghp_" + strings.Repeat("NewlineLess7", 3)
	blobFragment := blobSecret[len(blobSecret)-22:]
	blob := strings.Repeat("A", 2030) + blobSecret +
		strings.Repeat("B", installSmokeEvidenceLimit+2048-2030-len(blobSecret))
	if _, err := newlineless.Write([]byte(blob)); err != nil {
		t.Fatal(err)
	}
	path, warning = saveInstallSmokeEvidence(st, &newlineless, nil, state.ValidationExitSourceTarget)
	if warning != "" {
		t.Fatalf("newlineなしevidence warning = %q", warning)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), blobFragment) {
		t.Fatalf("newlineなし上限超過出力の部分secretがevidenceへrawで残っています: %q", string(data))
	}
}

func TestInstallSmokeFailureWithoutEvidenceSaveStaysFailed(t *testing.T) {
	cfg, st, failFlagPath, _ := newInstallSmokeEnv(t)
	if err := os.WriteFile(failFlagPath, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := st.Path("")
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })
	t.Cleanup(state.RedirectStatsWarnings(io.Discard))

	var stdout bytes.Buffer
	err := runInstallSmoke("worker", cfg, st, &stdout)
	var smokeFail *InstallSmokeError
	if !errors.As(err, &smokeFail) {
		t.Fatalf("install smoke失敗errorがInstallSmokeErrorではありません: %v", err)
	}
	if smokeFail.ExitSource != state.ValidationExitSourceTarget || smokeFail.ExitCode != 1 {
		t.Fatalf("exit = %d/%s", smokeFail.ExitCode, smokeFail.ExitSource)
	}
	if smokeFail.Evidence != "" || smokeFail.EvidenceWarning == "" {
		t.Fatalf("evidence = %q warning = %q", smokeFail.Evidence, smokeFail.EvidenceWarning)
	}
	body := buildProcessError(err)
	if body.Kind != errorKindInstallSmokeFailed {
		t.Fatalf("process error kind = %q", body.Kind)
	}
	if _, present := body.Detail["evidence"]; present {
		t.Fatalf("evidence保存失敗時にlocatorが残っています: %#v", body.Detail)
	}
	if warning, _ := body.Detail["evidence_warning"].(string); warning == "" {
		t.Fatalf("evidence保存失敗がprocess errorへ伝搬していません: %#v", body.Detail)
	}
}

func TestInstallSmokeLaunchFailureRecordsWrapperEvidence(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "sk-proj-AbC12345xY-repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	writeInstallSmokeScript(t, repoRoot, "#!/bin/sh\nexit 1\n")
	script := filepath.Join(repoRoot, "tests", "install_smoke.sh")
	if err := os.Chmod(script, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, st := newInstallSmokeStateEnv(t, repoRoot)

	var stdout bytes.Buffer
	err := runInstallSmoke("parent", cfg, st, &stdout)
	var smokeFail *InstallSmokeError
	if !errors.As(err, &smokeFail) {
		t.Fatalf("install smoke失敗errorがInstallSmokeErrorではありません: %v", err)
	}
	if smokeFail.ExitSource != state.ValidationExitSourceWrapper || smokeFail.ExitCode != 1 {
		t.Fatalf("exit = %d/%s", smokeFail.ExitCode, smokeFail.ExitSource)
	}
	data, err := os.ReadFile(smokeFail.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "permission denied") {
		t.Fatalf("evidenceにwrapper起動失敗の原因が含まれません: %q", string(data))
	}
	if strings.Contains(string(data), "sk-proj-AbC12345xY") {
		t.Fatalf("wrapper起動失敗error文のtokenがsanitizationされていません: %q", string(data))
	}
	record := readSingleStandaloneValidation(t, st)
	if record.Validation.Result != "fail" ||
		record.Validation.ExitSource != state.ValidationExitSourceWrapper ||
		record.Validation.Evidence != smokeFail.Evidence {
		t.Fatalf("standalone validation = %#v", record.Validation)
	}
}

func TestInstallSmokeSuccessWritesNoEvidenceArtifact(t *testing.T) {
	cfg, st, _, _ := newInstallSmokeEnv(t)

	var stdout bytes.Buffer
	if err := runInstallSmoke("worker", cfg, st, &stdout); err != nil {
		t.Fatal(err)
	}
	if err := validateSingleMachineJSONObject(stdout.Bytes()); err != nil {
		t.Fatalf("install smoke成功時のmachine stdoutが単一JSON objectではありません: %v: %q", err, stdout.String())
	}
	if _, err := os.Stat(st.Path(installSmokeRunDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("成功runでevidence artifactが作成されています: %v", err)
	}
}

func TestInstallSmokeEvidenceRetentionKeepsRecentRuns(t *testing.T) {
	cfg, st, failFlagPath, _ := newInstallSmokeEnv(t)
	if err := os.WriteFile(failFlagPath, []byte("fail\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	for range retainedInstallSmokeEvidenceRuns + 2 {
		if err := runInstallSmoke("worker", cfg, st, &stdout); err == nil {
			t.Fatal("install smoke失敗がerrorを返していません")
		}
	}
	entries, err := os.ReadDir(st.Path(installSmokeRunDirectory))
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, entry := range entries {
		if entry.IsDir() && validValidationRunID(entry.Name()) {
			runs++
		}
	}
	if runs != retainedInstallSmokeEvidenceRuns+1 {
		t.Fatalf("保持されたevidence run数 = %d want %d", runs, retainedInstallSmokeEvidenceRuns+1)
	}
}

func readSingleStandaloneValidation(t *testing.T, st *state.StateStore) state.TaskEventRecord {
	t.Helper()
	data, err := os.ReadFile(st.Path("validation-standalone.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := state.ParseTaskEventLine(data)
	if err != nil {
		t.Fatal(err)
	}
	if record.Kind != "validation" || record.Validation == nil {
		t.Fatalf("record = %#v", record)
	}
	return record
}
