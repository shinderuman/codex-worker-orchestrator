package app

import (
	"strconv"
	"strings"
)

func parentReviewQuotedDiffFileSection(body, path string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for index, line := range lines {
		if start >= 0 && strings.HasPrefix(line, "diff --git ") {
			return strings.Join(lines[start:index], "\n")
		}
		if parentReviewDiffHeaderIncludesPath(line, path) {
			start = index
		}
	}
	if start < 0 {
		return ""
	}
	return strings.Join(lines[start:], "\n")
}

func parentReviewDiffHeaderIncludesPath(line, path string) bool {
	const prefix = "diff --git "
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	rest := strings.TrimPrefix(line, prefix)
	if strings.HasPrefix(rest, "a/"+path+" b/") || strings.HasSuffix(rest, " b/"+path) {
		return true
	}
	oldPath, newPath, ok := parentReviewDiffHeaderPaths(line)
	return ok && (oldPath == path || newPath == path)
}

func parentReviewDiffHeaderPaths(line string) (string, string, bool) {
	const prefix = "diff --git "
	if !strings.HasPrefix(line, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(line, prefix)
	oldPath, rest, ok := parentReviewDiffHeaderToken(rest)
	if !ok {
		return "", "", false
	}
	newPath, rest, ok := parentReviewDiffHeaderToken(rest)
	if !ok || strings.TrimSpace(rest) != "" {
		return "", "", false
	}
	oldPath, oldOK := parentReviewStripDiffPrefix(oldPath, "a/")
	newPath, newOK := parentReviewStripDiffPrefix(newPath, "b/")
	return oldPath, newPath, oldOK && newOK
}

func parentReviewDiffHeaderToken(input string) (string, string, bool) {
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
	end, ok := parentReviewQuotedTokenEnd(input)
	if !ok {
		return "", "", false
	}
	value, err := strconv.Unquote(input[:end])
	if err != nil {
		return "", "", false
	}
	return value, input[end:], true
}

func parentReviewQuotedTokenEnd(input string) (int, bool) {
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

func parentReviewStripDiffPrefix(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
}
