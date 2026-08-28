from pathlib import Path

path = Path("glm-worker/internal/workflow/decision_boundary_review_test.go")
text = path.read_text()
old = '''    checkpoint, _, _, err := w.buildReviewCheckpoint("add operation telemetry", workerResult, 1, 0)
    if err != nil {
        t.Fatal(err)
    }
    if len(r.prompts) != 0 {
        t.Fatalf("building boundary context must not add a model call: phases=%v", r.phases)
    }
    if !strings.Contains(checkpoint.Prompt, solDecisionBoundaryReviewMarker) ||
        !strings.Contains(checkpoint.Prompt, "UNRESOLVED_AXES: public-surface") ||
        !strings.Contains(checkpoint.Prompt, "internal/timeline/timeline.go") {
        t.Fatalf("reviewer did not receive actual-diff boundary context: %s", checkpoint.Prompt)
    }
'''
new = '''    paths, err := collectChangedPaths(repo, baseline)
    if err != nil {
        t.Fatal(err)
    }
    if len(paths) != 1 || paths[0] != "internal/timeline/timeline.go" {
        t.Fatalf("actual changed paths = %v", paths)
    }
    boundary, err := w.reviewerDecisionBoundaryContext(task)
    if err != nil {
        t.Fatal(err)
    }
    if len(r.prompts) != 0 {
        t.Fatalf("boundary evidence collection must not add a model call: phases=%v", r.phases)
    }
    if !strings.Contains(boundary, solDecisionBoundaryReviewMarker) ||
        !strings.Contains(boundary, "UNRESOLVED_AXES: public-surface") {
        t.Fatalf("reviewer boundary context = %s", boundary)
    }
'''
if text.count(old) != 1:
    raise SystemExit(f"Task011 regression anchor count {text.count(old)} != 1")
path.write_text(text.replace(old, new, 1))
