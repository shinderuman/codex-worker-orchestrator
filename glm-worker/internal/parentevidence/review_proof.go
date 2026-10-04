package parentevidence

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reviewtarget"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (p *Projector) markReviewProof(parts []Part) error {
	binding, err := p.st.CurrentParentReviewBinding()
	if err != nil {
		return err
	}
	if binding == nil {
		return nil
	}
	claims, err := p.reviewCoverageClaims(binding.Targets, parts)
	if err != nil {
		return err
	}
	_, err = p.st.AccumulateParentReviewEvidenceCoverage(binding.ID, p.ownerCallID, p.leaseEpoch, claims)
	return err
}

func (p *Projector) reviewCoverageClaims(targets []string, parts []Part) ([]state.ParentReviewTargetCoverageClaim, error) {
	claims := make([]state.ParentReviewTargetCoverageClaim, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if _, duplicate := seen[target]; duplicate {
			continue
		}
		claim, ok := reviewClaimForTarget(target, parts)
		if !ok {
			var err error
			claim, ok, err = p.deliveredSourceClaimForTarget(target, parts)
			if err != nil {
				return nil, err
			}
		}
		if !ok {
			continue
		}
		seen[target] = struct{}{}
		claims = append(claims, state.ParentReviewTargetCoverageClaim{Target: target, Evidence: claim})
	}
	return claims, nil
}

func reviewClaimForTarget(target string, parts []Part) (state.ParentReviewEvidenceClaim, bool) {
	for _, part := range parts {
		if part.Status != PartProjected || part.Digest == "" {
			continue
		}
		if part.Source != nil {
			source, ok := sourceForReviewClaim(part, parts)
			if ok && reviewSourceCoversTarget(target, source) {
				return state.ParentReviewEvidenceClaim{Kind: "source", Digest: part.Digest, Locator: part.Locator}, true
			}
		}
		if part.Diff != nil && part.Diff.Body != "" && ReviewDiffCoversTarget(target, *part.Diff) {
			return state.ParentReviewEvidenceClaim{Kind: "diff", Digest: part.Digest, Locator: part.Locator}, true
		}
	}
	return state.ParentReviewEvidenceClaim{}, false
}

func (p *Projector) deliveredSourceClaimForTarget(target string, parts []Part) (state.ParentReviewEvidenceClaim, bool, error) {
	for _, part := range parts {
		if part.Status != PartProjected || part.Reason != UnchangedReason || part.Digest == "" || part.Source == nil || part.Source.Content != "" {
			continue
		}
		covered, err := sourceMetadataCoversTarget(p.repoRoot, target, *part.Source)
		if err != nil {
			return state.ParentReviewEvidenceClaim{}, false, err
		}
		if !covered {
			continue
		}
		_, delivered, err := p.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceSource, part.Digest)
		if err != nil {
			return state.ParentReviewEvidenceClaim{}, false, err
		}
		if delivered {
			return state.ParentReviewEvidenceClaim{Kind: "source", Digest: part.Digest, Locator: part.Locator}, true, nil
		}
	}
	return state.ParentReviewEvidenceClaim{}, false, nil
}

func sourceMetadataCoversTarget(repoRoot, raw string, source SourceBody) (bool, error) {
	target, err := reviewtarget.ParseTarget(raw)
	if err != nil || target.Path != source.Path {
		return false, nil
	}
	switch target.Kind {
	case reviewtarget.LocatorLineRange:
		return source.LineStart <= target.LineStart && source.LineEnd >= target.LineEnd, nil
	case reviewtarget.LocatorGoSymbol:
		absolute, err := joinRoot(repoRoot, target.Path)
		if err != nil {
			return false, err
		}
		content, err := os.ReadFile(absolute)
		if err != nil {
			return false, fmt.Errorf("read delivered review symbol source %s: %w", target.Path, err)
		}
		declaration, err := reviewtarget.FindGoDeclaration(content, target.Locator)
		if err != nil {
			return false, nil
		}
		return source.LineStart <= declaration.LineStart && source.LineEnd >= declaration.LineEnd, nil
	default:
		return false, nil
	}
}

func sourceForReviewClaim(part Part, parts []Part) (SourceBody, bool) {
	if part.Source == nil {
		return SourceBody{}, false
	}
	source := *part.Source
	if source.Content != "" {
		return source, true
	}
	if part.Status != PartProjected || part.Reason != UnchangedReason {
		return SourceBody{}, false
	}
	for _, candidate := range parts {
		if candidate.Source == nil || candidate.Digest != part.Digest || candidate.Source.Content == "" || candidate.Status != PartProjected {
			continue
		}
		source.Content = candidate.Source.Content
		return source, true
	}
	return SourceBody{}, false
}

func reviewSourceCoversTarget(raw string, source SourceBody) bool {
	target, err := reviewtarget.ParseTarget(raw)
	if err != nil || target.Path != source.Path {
		return false
	}
	switch target.Kind {
	case reviewtarget.LocatorLineRange:
		return source.LineStart <= target.LineStart && source.LineEnd >= target.LineEnd
	case reviewtarget.LocatorGoSymbol:
		return sourceCoversSymbol(target, source.Content)
	default:
		return false
	}
}

func sourceCoversSymbol(target reviewtarget.Target, content string) bool {
	declaration, err := reviewtarget.FindGoDeclaration([]byte(content), target.Locator)
	if err == nil {
		return declaration.Locator == target.Locator
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	declaration, err = reviewtarget.FindGoDeclaration([]byte("package reviewevidence\n"+trimmed), target.Locator)
	return err == nil && declaration.Locator == target.Locator
}

func ReviewDiffCoversTarget(raw string, diff DiffBody) bool {
	target, err := reviewtarget.ParseTarget(raw)
	if err != nil {
		return false
	}
	for _, file := range diff.Files {
		if reviewDiffFileCoversTarget(target, file, diff.Body) {
			return true
		}
	}
	return false
}

func reviewDiffFileCoversTarget(target reviewtarget.Target, file DiffFile, body string) bool {
	if target.Path != file.Path || file.Status == "unknown" {
		return false
	}
	if file.HeadBlob == "" && file.IndexBlob == "" && file.WorktreeSHA == "" {
		return false
	}
	section := ReviewDiffFileSection(body, file.Path)
	if section == "" {
		return false
	}
	switch target.Kind {
	case reviewtarget.LocatorLineRange:
		return reviewDiffSectionCoversLines(section, target.LineStart, target.LineEnd)
	case reviewtarget.LocatorGoSymbol:
		return reviewDiffSectionDeletesFile(section) && sourceCoversSymbol(target, deletedReviewDiffSource(section))
	case reviewtarget.LocatorWholeDiff:
		return true
	default:
		return false
	}
}

func ReviewDiffFileSection(body, path string) string {
	if body == "" || path == "" {
		return ""
	}
	return quotedDiffFileSection(body, path)
}

func reviewDiffSectionCoversLines(section string, start, end int) bool {
	deleted := reviewDiffSectionDeletesFile(section)
	for _, line := range strings.Split(section, "\n") {
		hunkStart, hunkEnd, ok := reviewDiffHunkRange(line, deleted)
		if ok && start >= hunkStart && end <= hunkEnd {
			return true
		}
	}
	return false
}

func reviewDiffSectionDeletesFile(section string) bool {
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "+++ ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "+++ ")) == "/dev/null"
		}
	}
	return false
}

func deletedReviewDiffSource(section string) string {
	var source strings.Builder
	inHunk := false
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			inHunk = true
			continue
		}
		if !inHunk || strings.HasPrefix(line, `\ No newline at end of file`) {
			continue
		}
		switch {
		case strings.HasPrefix(line, "-"):
			source.WriteString(strings.TrimPrefix(line, "-"))
			source.WriteByte('\n')
		case strings.HasPrefix(line, " "):
			source.WriteString(strings.TrimPrefix(line, " "))
			source.WriteByte('\n')
		}
	}
	return source.String()
}

func reviewDiffHunkRange(line string, oldSide bool) (int, int, bool) {
	if !strings.HasPrefix(line, "@@ ") {
		return 0, 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, 0, false
	}
	token := fields[2]
	prefix := "+"
	if oldSide {
		token = fields[1]
		prefix = "-"
	}
	if !strings.HasPrefix(token, prefix) {
		return 0, 0, false
	}
	value := strings.TrimPrefix(token, prefix)
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
