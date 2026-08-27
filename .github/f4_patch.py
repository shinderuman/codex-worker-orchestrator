from pathlib import Path

path = Path("glm-worker/internal/workflow/workflow.go")
text = path.read_text()
old = '''func (w *Workflow) computeEffectiveRisk(workerResult packet.Result, autoFixes int, hasDecision bool, hasPriorReview bool) effectiveRisk {
\tsp := w.selfProtectionNow()
\tif !reviewNeedsHighRiskFloor(workerResult, autoFixes, hasDecision, hasPriorReview) && !sp.High {
\t\treturn effectiveRisk{high: false}
\t}
\tvar sources []string
\tif workerResult.Risk == packet.RiskHigh {
\t\tsources = append(sources, "worker-declared")
\t}
\tif autoFixes > 0 {
\t\tsources = append(sources, "auto-fix")
\t}
\tif hasDecision {
\t\tsources = append(sources, "decision")
\t}
\tif hasPriorReview {
\t\tsources = append(sources, "prior-review")
\t}
\tif sp.High {
\t\tsources = append(sources, "self-protection:"+sp.Source)
\t}
\treturn effectiveRisk{high: true, source: strings.Join(sources, ";")}
}

func (w *Workflow) selfProtectionNow() selfProtectionDecision {
\tbaselineHead, _ := w.state.Read("baseline-head")
\tpaths, err := w.collectChangedPaths(w.config.RepoRoot, baselineHead)
\tif err != nil {
\t\treturn selfProtectionDecision{High: true, Source: "classify-error", HitPath: err.Error()}
\t}
\treturn classifySelfProtection(paths)
}
'''
new = '''func (w *Workflow) computeEffectiveRisk(workerResult packet.Result, autoFixes int, hasDecision bool, hasPriorReview bool) effectiveRisk {
\tsp, qe := w.riskSurfaceDecisions()
\tif !reviewNeedsHighRiskFloor(workerResult, autoFixes, hasDecision, hasPriorReview) && !sp.High && !qe.High {
\t\treturn effectiveRisk{high: false}
\t}
\tvar sources []string
\tif workerResult.Risk == packet.RiskHigh {
\t\tsources = append(sources, "worker-declared")
\t}
\tif autoFixes > 0 {
\t\tsources = append(sources, "auto-fix")
\t}
\tif hasDecision {
\t\tsources = append(sources, "decision")
\t}
\tif hasPriorReview {
\t\tsources = append(sources, "prior-review")
\t}
\tif sp.High {
\t\tsources = append(sources, "self-protection:"+sp.Source)
\t}
\tif qe.High {
\t\tsources = append(sources, "quality-evidence:"+qe.Source)
\t}
\treturn effectiveRisk{high: true, source: strings.Join(sources, ";")}
}

func (w *Workflow) riskSurfaceDecisions() (selfProtectionDecision, qualityEvidenceDecision) {
\tbaselineHead, _ := w.state.Read("baseline-head")
\tpaths, err := w.collectChangedPaths(w.config.RepoRoot, baselineHead)
\tif err != nil {
\t\treturn selfProtectionDecision{High: true, Source: "classify-error", HitPath: err.Error()}, qualityEvidenceDecision{}
\t}
\tsp := classifySelfProtection(paths)
\tqe, err := classifyQualityEvidence(w.config.RepoRoot, baselineHead, paths)
\tif err != nil {
\t\tqe = qualityEvidenceDecision{High: true, Source: "classify-error", HitPath: err.Error()}
\t}
\treturn sp, qe
}
'''
if text.count(old) != 1:
    raise SystemExit(f"workflow anchor count={text.count(old)}")
path.write_text(text.replace(old, new, 1))
