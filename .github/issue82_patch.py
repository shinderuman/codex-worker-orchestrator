from pathlib import Path

p = Path('glm-worker/internal/app/output.go')
text = p.read_text()
old = '''func parentAccept(st *state.StateStore, stdout io.Writer) error {
	resolved, err := st.RecordParentOutcome(state.ParentOutcomeAccepted, "")
	if err != nil {
		return err
	}
	return writeJSON(stdout, acceptOutput{Accepted: resolved})
}'''
new = '''func parentAccept(st *state.StateStore, stdout io.Writer) error {
	resolved, err := st.RecordParentOutcome(state.ParentOutcomeAccepted, "")
	if err != nil {
		return err
	}
	if resolved {
		if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
			return err
		}
	}
	return writeJSON(stdout, acceptOutput{Accepted: resolved})
}'''
if old not in text:
    raise SystemExit('parentAccept block not found')
p.write_text(text.replace(old, new, 1))

p = Path('glm-worker/internal/app/parent_review_test.go')
text = p.read_text()
old = '''	if got := st.TaskStatus(); got != state.TaskStatusWaitingSolReview {
		t.Fatalf("accept後のstale lifecycle status = %q want %q", got, state.TaskStatusWaitingSolReview)
	}'''
new = '''	if got := st.TaskStatus(); got != state.TaskStatusComplete {
		t.Fatalf("accept後のlifecycle status = %q want %q", got, state.TaskStatusComplete)
	}
	completedStats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if completedStats.Status != state.TaskStatusComplete {
		t.Fatalf("accept後のstats status = %q want %q", completedStats.Status, state.TaskStatusComplete)
	}'''
if old not in text:
    raise SystemExit('stale lifecycle expectation not found')
text = text.replace(old, new, 1)

needle = '''func TestExecuteParentReviewAcceptIsSingleUse(t *testing.T) {
	cfg, _ := newParentReviewOpportunity(t)
	applyParentReviewFix(t, cfg)
	if first := executeAccept(t, cfg); !first.Accepted {
		t.Fatal("最初のacceptが確定されませんでした")
	}
	if retry := executeAccept(t, cfg); retry.Accepted {
		t.Fatal("同一opportunityのaccept再実行が二重確定されました")
	}
}'''
replacement = needle + '''

func TestExecuteParentReviewAcceptCompletesOnlyResolvedReview(t *testing.T) {
	cfg, st := newParentReviewOpportunity(t)
	if accept := executeAccept(t, cfg); !accept.Accepted {
		t.Fatal("open reviewをacceptできませんでした")
	}
	if got := st.TaskStatus(); got != state.TaskStatusComplete {
		t.Fatalf("accepted status = %q", got)
	}

	cfg = newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if accept := executeAccept(t, cfg); accept.Accepted {
		t.Fatal("open reviewなしのacceptが確定されました")
	}
	if got := st.TaskStatus(); got != state.TaskStatusWaitingSolReview {
		t.Fatalf("no-op accept changed status = %q", got)
	}
}'''
if needle not in text:
    raise SystemExit('single use test block not found')
p.write_text(text.replace(needle, replacement, 1))
