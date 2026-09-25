package parentevidence

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func PrintReviewEvidence(repoRoot string, st *state.StateStore, stdout io.Writer) error {
	manifest, err := BuildReviewManifest(st)
	if err != nil {
		return err
	}
	ownerCallID, err := state.NewUUID()
	if err != nil {
		return err
	}
	projector := NewProjector(repoRoot, st, ownerCallID, Providers{})
	projector.Project(manifest)
	return projector.Commit(stdout, manifest.Reason)
}

func BuildReviewManifest(st *state.StateStore) (Manifest, error) {
	binding, err := st.CurrentParentReviewBinding()
	if err != nil {
		return Manifest{}, err
	}
	if binding == nil {
		return Manifest{}, fmt.Errorf("review evidence requires an open NEEDS_SOL_REVIEW binding")
	}
	question := strings.TrimSpace(binding.SolQuestion)
	if question == "" || len(binding.Targets) == 0 {
		return Manifest{}, fmt.Errorf("review evidence binding has no semantic question or targets")
	}

	manifest := Manifest{
		Version: ManifestVersion,
		Reason:  "parent-review-targets:" + binding.ID,
	}
	seenSource := map[string]struct{}{}
	diffPaths := map[string]struct{}{}
	for _, target := range binding.Targets {
		path, locator, err := ReviewTarget(target)
		if err != nil {
			return Manifest{}, err
		}
		if start, end, ok := NumericRange(locator); ok {
			key := fmt.Sprintf("%s:%d-%d", path, start, end)
			if _, exists := seenSource[key]; exists {
				continue
			}
			seenSource[key] = struct{}{}
			manifest.Source = append(manifest.Source, SourceRequest{
				Question:    question,
				Path:        path,
				LineStart:   start,
				LineEnd:     end,
				BudgetBytes: MaxBudgetBytes,
			})
			continue
		}
		diffPaths[path] = struct{}{}
	}
	paths := make([]string, 0, len(diffPaths))
	for path := range diffPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		manifest.Diff = append(manifest.Diff, DiffRequest{
			Question:    question,
			Paths:       []string{path},
			BudgetBytes: MaxBudgetBytes,
		})
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ReviewTarget(target string) (string, string, error) {
	return reviewtarget.Parse(target)
}

func (p *Projector) markReviewProof(parts []Part) error {
	binding, err := p.st.CurrentParentReviewBinding()
	if err != nil {
		return err
	}
	if binding == nil {
		return nil
	}
	claims, complete := ReviewClaims(binding.Targets, parts)
	if !complete {
		return nil
	}
	return p.st.MarkParentReviewEvidence(binding.ID, p.ownerCallID, claims)
}

func ReviewClaims(targets []string, parts []Part) ([]state.ParentReviewEvidenceClaim, bool) {
	claims := make([]state.ParentReviewEvidenceClaim, 0, len(targets))
	seen := make(map[string]struct{})
	for _, target := range targets {
		claim, ok := reviewClaimForTarget(target, parts)
		if !ok {
			return nil, false
		}
		key := claim.Kind + "\x00" + claim.Digest + "\x00" + claim.Locator
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		claims = append(claims, claim)
	}
	return claims, len(claims) > 0
}

func reviewClaimForTarget(target string, parts []Part) (state.ParentReviewEvidenceClaim, bool) {
	for _, part := range parts {
		if part.Digest == "" {
			continue
		}
		if part.Source != nil && part.Source.Content != "" && reviewSourceCoversTarget(target, *part.Source) {
			return state.ParentReviewEvidenceClaim{Kind: "source", Digest: part.Digest, Locator: part.Locator}, true
		}
		if part.Diff != nil && part.Diff.Body != "" && ReviewDiffCoversTarget(target, *part.Diff) {
			return state.ParentReviewEvidenceClaim{Kind: "diff", Digest: part.Digest, Locator: part.Locator}, true
		}
	}
	return state.ParentReviewEvidenceClaim{}, false
}

func reviewSourceCoversTarget(target string, source SourceBody) bool {
	if !reviewTargetMatchesPath(target, source.Path) {
		return false
	}
	suffix := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(target), source.Path))
	if !strings.HasPrefix(suffix, ":") {
		return false
	}
	locator := strings.TrimSpace(strings.TrimPrefix(suffix, ":"))
	start, end, ok := NumericRange(locator)
	return ok && source.LineStart <= start && source.LineEnd >= end
}

func ReviewDiffCoversTarget(target string, diff DiffBody) bool {
	target = strings.TrimSpace(target)
	for _, file := range diff.Files {
		if reviewDiffFileCoversTarget(target, file, diff.Body) {
			return true
		}
	}
	return false
}

func reviewDiffFileCoversTarget(target string, file DiffFile, body string) bool {
	if !reviewTargetMatchesPath(target, file.Path) || file.Status == "unknown" {
		return false
	}
	if file.HeadBlob == "" && file.IndexBlob == "" && file.WorktreeSHA == "" {
		return false
	}
	section := ReviewDiffFileSection(body, file.Path)
	if section == "" {
		return false
	}
	suffix := strings.TrimSpace(strings.TrimPrefix(target, file.Path))
	if suffix == "" {
		return true
	}
	if !strings.HasPrefix(suffix, ":") {
		return false
	}
	locator := strings.TrimSpace(strings.TrimPrefix(suffix, ":"))
	if start, end, ok := NumericRange(locator); ok {
		return reviewDiffSectionCoversLines(section, start, end)
	}
	return locator != "" && strings.Contains(section, locator)
}

func ReviewDiffFileSection(body, path string) string {
	if body == "" || path == "" {
		return ""
	}
	return quotedDiffFileSection(body, path)
}

func reviewDiffSectionCoversLines(section string, start, end int) bool {
	for _, line := range strings.Split(section, "\n") {
		hunkStart, hunkEnd, ok := reviewDiffHunkCurrentRange(line)
		if ok && start >= hunkStart && end <= hunkEnd {
			return true
		}
	}
	return false
}

func reviewDiffHunkCurrentRange(line string) (int, int, bool) {
	if !strings.HasPrefix(line, "@@ ") {
		return 0, 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return 0, 0, false
	}
	value := strings.TrimPrefix(fields[2], "+")
	parts := strings.SplitN(value, ",", 2)
	start, ok := positiveLine(parts[0])
	if !ok {
		return 0, 0, false
	}
	count := 1
	if len(parts) == 2 {
		parsed, err := strconv.Atoi(parts[1])
		if err != nil || parsed <= 0 {
			return 0, 0, false
		}
		count = parsed
	}
	return start, start + count - 1, true
}

func reviewTargetMatchesPath(target, path string) bool {
	target = strings.TrimSpace(target)
	if target == path {
		return true
	}
	for _, separator := range []string{":", " ", ","} {
		if strings.HasPrefix(target, path+separator) {
			return true
		}
	}
	return false
}

func NumericRange(locator string) (int, int, bool) {
	token := numericRangeToken(locator)
	if token == "" {
		return 0, 0, false
	}
	values := strings.Split(token, "-")
	if len(values) > 2 || values[0] == "" {
		return 0, 0, false
	}
	start, ok := positiveLine(values[0])
	if !ok {
		return 0, 0, false
	}
	if len(values) == 1 {
		return start, start, true
	}
	end, ok := positiveLine(values[1])
	if !ok || end < start {
		return 0, 0, false
	}
	return start, end, true
}

func numericRangeToken(locator string) string {
	end := 0
	for end < len(locator) {
		c := locator[end]
		if (c < '0' || c > '9') && c != '-' {
			break
		}
		end++
	}
	return locator[:end]
}

func positiveLine(value string) (int, bool) {
	line, err := strconv.Atoi(value)
	return line, err == nil && line > 0
}

func quotedDiffFileSection(body, path string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for index, line := range lines {
		if start >= 0 && strings.HasPrefix(line, "diff --git ") {
			return strings.Join(lines[start:index], "\n")
		}
		if diffHeaderIncludesPath(line, path) {
			start = index
		}
	}
	if start < 0 {
		return ""
	}
	return strings.Join(lines[start:], "\n")
}

func diffHeaderIncludesPath(line, path string) bool {
	const prefix = "diff --git "
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	rest := strings.TrimPrefix(line, prefix)
	if strings.HasPrefix(rest, "a/"+path+" b/") || strings.HasSuffix(rest, " b/"+path) {
		return true
	}
	oldPath, newPath, ok := diffHeaderPaths(line)
	return ok && (oldPath == path || newPath == path)
}

func diffHeaderPaths(line string) (string, string, bool) {
	const prefix = "diff --git "
	if !strings.HasPrefix(line, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(line, prefix)
	oldPath, rest, ok := diffHeaderToken(rest)
	if !ok {
		return "", "", false
	}
	newPath, rest, ok := diffHeaderToken(rest)
	if !ok || strings.TrimSpace(rest) != "" {
		return "", "", false
	}
	oldPath, oldOK := stripDiffPrefix(oldPath, "a/")
	newPath, newOK := stripDiffPrefix(newPath, "b/")
	return oldPath, newPath, oldOK && newOK
}

func diffHeaderToken(input string) (string, string, bool) {
	input = strings.TrimLeft(input, " ")
	if input == "" {
		return "", "", false
	}
	if input[0] != '"' {
		end := strings.IndexByte(input, ' ')
		if end < 0 {
			return input, "", true
		}
		return input[:end], input[end:], true
	}
	end, ok := quotedTokenEnd(input)
	if !ok {
		return "", "", false
	}
	value, err := strconv.Unquote(input[:end])
	if err != nil {
		return "", "", false
	}
	return value, input[end:], true
}

func quotedTokenEnd(input string) (int, bool) {
	escaped := false
	for index := 1; index < len(input); index++ {
		switch {
		case escaped:
			escaped = false
		case input[index] == '\\':
			escaped = true
		case input[index] == '"':
			return index + 1, true
		}
	}
	return 0, false
}

func stripDiffPrefix(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
}
