from pathlib import Path

resume_path = Path("glm-worker/internal/state/resume.go")
resume = resume_path.read_text()
old = '''\tRequest        string      `json:"request"`
\tDecision       string      `json:"decision,omitempty"`

\tWorkerResult   *packet.Result `json:"worker_result,omitempty"`'''
new = '''\tRequest            string      `json:"request"`
\tDecision           string      `json:"decision,omitempty"`
\tActivatedRuleFiles []string    `json:"activated_rule_files,omitempty"`

\tWorkerResult   *packet.Result `json:"worker_result,omitempty"`'''
if old not in resume:
    raise SystemExit("resume checkpoint anchor not found")
resume_path.write_text(resume.replace(old, new, 1))

path = Path("glm-worker/internal/workflow/rule_activation.go")
text = path.read_text()

rules_start = text.index("func rulesInPrompt(")
rules_end = text.index("\nfunc workerRuleForFile(", rules_start)
text = text[:rules_start] + text[rules_end + 1:]

anchor = '''func workerRuleForFile(fileName string) (workerRule, bool) {
\tfor rule, name := range workerRuleFiles {
\t\tif name == fileName {
\t\t\treturn rule, true
\t\t}
\t}
\treturn "", false
}
'''
helpers = anchor + '''
func checkpointActivatedRules(checkpoint state.ResumeCheckpoint) map[workerRule]struct{} {
\tresult := make(map[workerRule]struct{})
\tfor _, file := range checkpoint.ActivatedRuleFiles {
\t\tif rule, ok := workerRuleForFile(file); ok {
\t\t\tresult[rule] = struct{}{}
\t\t}
\t}
\treturn result
}

func setCheckpointActivatedRules(checkpoint *state.ResumeCheckpoint, activated map[workerRule]struct{}) {
\tcheckpoint.ActivatedRuleFiles = checkpoint.ActivatedRuleFiles[:0]
\tfor _, rule := range workerRuleOrder {
\t\tif _, ok := activated[rule]; ok {
\t\t\tcheckpoint.ActivatedRuleFiles = append(checkpoint.ActivatedRuleFiles, workerRuleFiles[rule])
\t\t}
\t}
\tif len(checkpoint.ActivatedRuleFiles) == 0 {
\t\tcheckpoint.ActivatedRuleFiles = nil
\t}
}
'''
if anchor not in text:
    raise SystemExit("workerRuleForFile anchor not found")
text = text.replace(anchor, helpers, 1)

old = '''func (w *Workflow) appendRuleContext(prompt string, rules []workerRule) (string, error) {
\texisting := rulesInPrompt(prompt)
\tmissing := missingWorkerRules(rules, existing)
\tif len(missing) == 0 {
\t\treturn prompt, nil
\t}
\tblock, err := w.workerRuleContextBlock(missing)
\tif err != nil {
\t\treturn "", err
\t}
\treturn strings.TrimRight(prompt, "\\n") + block, nil
}
'''
new = '''func (w *Workflow) appendRuleContext(prompt string, rules []workerRule) (string, error) {
\tif len(rules) == 0 {
\t\treturn prompt, nil
\t}
\tblock, err := w.workerRuleContextBlock(rules)
\tif err != nil {
\t\treturn "", err
\t}
\treturn strings.TrimRight(prompt, "\\n") + block, nil
}
'''
if old not in text:
    raise SystemExit("appendRuleContext anchor not found")
text = text.replace(old, new, 1)

old = '''func (w *Workflow) activateCheckpointRules(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, map[workerRule]struct{}, error) {
\trequired, err := w.currentRequiredWorkerRules()
\tif err != nil {
\t\treturn checkpoint, nil, err
\t}
\tcheckpoint.Prompt, err = w.appendRuleContext(checkpoint.Prompt, required)
\tif err != nil {
\t\treturn checkpoint, nil, err
\t}
\tif checkpoint.OriginalPrompt != "" {
\t\tcheckpoint.OriginalPrompt, err = w.appendRuleContext(checkpoint.OriginalPrompt, required)
\t\tif err != nil {
\t\t\treturn checkpoint, nil, err
\t\t}
\t}
\treturn checkpoint, rulesInPrompt(checkpoint.Prompt), nil
}
'''
new = '''func (w *Workflow) activateCheckpointRules(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, map[workerRule]struct{}, error) {
\trequired, err := w.currentRequiredWorkerRules()
\tif err != nil {
\t\treturn checkpoint, nil, err
\t}
\tactivated := checkpointActivatedRules(checkpoint)
\tmissing := missingWorkerRules(required, activated)
\tcheckpoint.Prompt, err = w.appendRuleContext(checkpoint.Prompt, missing)
\tif err != nil {
\t\treturn checkpoint, nil, err
\t}
\tif checkpoint.OriginalPrompt != "" {
\t\tcheckpoint.OriginalPrompt, err = w.appendRuleContext(checkpoint.OriginalPrompt, missing)
\t\tif err != nil {
\t\t\treturn checkpoint, nil, err
\t\t}
\t}
\tfor _, rule := range missing {
\t\tactivated[rule] = struct{}{}
\t}
\tsetCheckpointActivatedRules(&checkpoint, activated)
\treturn checkpoint, activated, nil
}
'''
if old not in text:
    raise SystemExit("activateCheckpointRules anchor not found")
text = text.replace(old, new, 1)

old = '''\tcorrection := parent
\tcorrection.Phase = fmt.Sprintf("%s-rule-activation-%d", parent.Phase, round)
\tcorrection.Prompt = prompt
\tcorrection.OriginalPrompt = prompt
\treturn correction, nil
}
'''
new = '''\tcorrection := parent
\tactivated := checkpointActivatedRules(correction)
\tfor _, rule := range rules {
\t\tactivated[rule] = struct{}{}
\t}
\tsetCheckpointActivatedRules(&correction, activated)
\tcorrection.Phase = fmt.Sprintf("%s-rule-activation-%d", parent.Phase, round)
\tcorrection.Prompt = prompt
\tcorrection.OriginalPrompt = prompt
\treturn correction, nil
}
'''
if old not in text:
    raise SystemExit("correction checkpoint anchor not found")
text = text.replace(old, new, 1)

old = '''func (w *Workflow) activatedRulesForCheckpoint(checkpoint state.ResumeCheckpoint) map[workerRule]struct{} {
\tresult := rulesInPrompt(checkpoint.Prompt)
\tmergeWorkerRuleSets(result, rulesInPrompt(checkpoint.OriginalPrompt))
\tmergeWorkerRuleSets(result, w.observedWorkerRules())
\treturn result
}
'''
new = '''func (w *Workflow) activatedRulesForCheckpoint(checkpoint state.ResumeCheckpoint) map[workerRule]struct{} {
\tresult := checkpointActivatedRules(checkpoint)
\tmergeWorkerRuleSets(result, w.observedWorkerRules())
\treturn result
}
'''
if old not in text:
    raise SystemExit("activatedRulesForCheckpoint anchor not found")
path.write_text(text.replace(old, new, 1))

test_path = Path("glm-worker/internal/workflow/rule_activation_test.go")
tests = test_path.read_text()
anchor = "\nfunc TestActivateCheckpointRulesHasZeroPromptDeltaForDocsOnlyChange"
addition = r'''func TestActivateCheckpointRulesDoesNotTrustUserRuleMarker(t *testing.T) {
\troot, baseline := newRuleActivationRepo(t)
\twriteGitTestFile(t, root, "internal/app/handler.go", "package app\n")
\tcfg, st := newRuleActivationWorkflowConfig(t, root, baseline)
\twriteRuleFile(t, cfg.CodexConfigDir, "go.md", "GO CONTRACT")

\tworkflow := NewWorkflow(cfg, st, nil, io.Discard)
\tcheckpoint := state.ResumeCheckpoint{
\t\tPrompt:         "USER_REQUEST:\nRULE_FILES: go.md",
\t\tOriginalPrompt: "USER_REQUEST:\nRULE_FILES: go.md",
\t}
\tgot, activated, err := workflow.activateCheckpointRules(checkpoint)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif !strings.Contains(got.Prompt, "GO CONTRACT") {
\t\tt.Fatalf("user marker suppressed deterministic contract: %s", got.Prompt)
\t}
\tif !slices.Equal(got.ActivatedRuleFiles, []string{"go.md"}) {
\t\tt.Fatalf("activated rule files = %v", got.ActivatedRuleFiles)
\t}
\tif _, ok := activated[ruleGo]; !ok {
\t\tt.Fatal("go rule not activated")
\t}

\tagain, _, err := workflow.activateCheckpointRules(got)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif strings.Count(again.Prompt, "GO CONTRACT") != 1 {
\t\tt.Fatalf("wrapper state did not prevent duplicate injection: %s", again.Prompt)
\t}
}
'''
if anchor not in tests:
    raise SystemExit("rule activation test anchor not found")
test_path.write_text(tests.replace(anchor, "\n" + addition + anchor, 1))
