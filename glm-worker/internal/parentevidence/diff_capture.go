package parentevidence

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type diffCheck struct {
	paths    []string
	question string
	digest   string
}

func exactDiffPath(path string) bool {
	if !RelativePath(path) || filepath.IsAbs(path) || strings.HasPrefix(path, ":") ||
		strings.HasSuffix(path, "/") || strings.ContainsAny(path, "\x00*?[]") ||
		path != filepath.ToSlash(filepath.Clean(path)) {
		return false
	}
	for _, component := range strings.Split(path, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func captureDiff(repoRoot string, paths []string) ([]DiffFile, string, error) {
	files, body, err := captureDiffOnce(repoRoot, paths)
	if err != nil {
		return nil, "", err
	}
	nextFiles, nextBody, err := captureDiffOnce(repoRoot, paths)
	if err != nil {
		return nil, "", err
	}
	if !reflect.DeepEqual(files, nextFiles) || body != nextBody {
		return nil, "", fmt.Errorf("diff source changed during capture; request fresh evidence")
	}
	return files, body, nil
}

func captureDiffOnce(repoRoot string, paths []string) ([]DiffFile, string, error) {
	files := make([]DiffFile, 0, len(paths))
	var untracked strings.Builder
	for _, path := range paths {
		file, content, err := collectDiffFile(repoRoot, path)
		if err != nil {
			return nil, "", err
		}
		if len(content) > maxDiffPatchBytes-untracked.Len() {
			return nil, "", fmt.Errorf("requested diff exceeds %d-byte aggregate patch execution limit at %q (HEAD %s, index %s, worktree %s)", maxDiffPatchBytes, path, file.HeadBlob, file.IndexBlob, file.WorktreeSHA)
		}
		files = append(files, file)
		untracked.WriteString(content)
	}
	return files, untracked.String(), nil
}

func collectDiffFile(repoRoot, path string) (DiffFile, string, error) {
	if !exactDiffPath(path) {
		return DiffFile{}, "", fmt.Errorf("invalid exact diff path %q", path)
	}
	headMode, headBlob, err := gitDiffEntry(repoRoot, path, false)
	if err != nil {
		return DiffFile{}, "", err
	}
	indexMode, indexBlob, err := gitDiffEntry(repoRoot, path, true)
	if err != nil {
		return DiffFile{}, "", err
	}
	worktreeSHA, worktreeMode, content, present, err := diffWorktreeObject(repoRoot, path)
	if err != nil {
		return DiffFile{}, "", fmt.Errorf("capture diff %q (HEAD %s mode %s, index %s mode %s): %w", path, headBlob, headMode, indexBlob, indexMode, err)
	}
	if headBlob == "" && indexBlob == "" && !present {
		return DiffFile{}, "", fmt.Errorf("diff path %q does not exist in HEAD, index or worktree", path)
	}
	file := DiffFile{
		Path:         path,
		HeadBlob:     headBlob,
		HeadMode:     headMode,
		IndexBlob:    indexBlob,
		IndexMode:    indexMode,
		WorktreeSHA:  worktreeSHA,
		WorktreeMode: worktreeMode,
	}
	rendered, patch, err := renderCapturedDiffFile(repoRoot, file, content, present)
	if err != nil {
		return DiffFile{}, "", fmt.Errorf("capture diff %q (HEAD %s, index %s, worktree %s mode %s): %w", path, headBlob, indexBlob, worktreeSHA, worktreeMode, err)
	}
	return rendered, patch, nil
}

func renderCapturedDiffFile(repoRoot string, file DiffFile, content []byte, present bool) (DiffFile, string, error) {
	if file.HeadBlob == "" && file.IndexBlob == "" {
		file.Status = "?"
		patch, err := untrackedDiffSection(file.Path, file.WorktreeMode, content)
		return file, patch, err
	}
	patch, err := capturedDiffPatch(repoRoot, file, content, present)
	if err != nil {
		return DiffFile{}, "", err
	}
	switch {
	case !present:
		file.Status = "D"
	case file.HeadBlob == "":
		file.Status = "A"
	case patch != "" || file.HeadMode != file.IndexMode || file.HeadBlob != file.IndexBlob:
		file.Status = "M"
	}
	file.Additions, file.Deletions = diffLineCounts(patch)
	return file, patch, nil
}

func gitDiffEntry(repoRoot, path string, index bool) (string, string, error) {
	args := []string{"ls-tree", "-z", "HEAD", "--", path}
	if index {
		args = []string{"ls-files", "-s", "-z", "--", path}
	}
	data, err := gitLiteralOutputIn(repoRoot, args...)
	if err != nil {
		return "", "", fmt.Errorf("read Git entry for %q: %w", path, err)
	}
	return parseDiffGitEntry(data, path, index)
}

func parseDiffGitEntry(data []byte, path string, index bool) (string, string, error) {
	var mode, object string
	if len(data) == 0 {
		return "", "", nil
	}
	for _, row := range bytes.Split(bytes.TrimSuffix(data, []byte{0}), []byte{0}) {
		meta, name, ok := bytes.Cut(row, []byte{'\t'})
		if !ok || string(name) != path || mode != "" {
			return "", "", fmt.Errorf("non-exact or ambiguous Git entry for %q", path)
		}
		fields := strings.Fields(string(meta))
		if !validDiffGitEntry(fields, index) {
			return "", "", fmt.Errorf("unsupported or unmerged Git entry for %q", path)
		}
		mode = fields[0]
		if index {
			object = fields[1]
		} else {
			object = fields[2]
		}
	}
	return mode, object, nil
}

func validDiffGitEntry(fields []string, index bool) bool {
	if len(fields) != 3 || !supportedDiffGitMode(fields[0]) {
		return false
	}
	if index {
		return fields[2] == "0"
	}
	return fields[1] == "blob"
}

func supportedDiffGitMode(mode string) bool {
	return mode == "100644" || mode == "100755" || mode == "120000"
}

func untrackedDiffSection(path, mode string, content []byte) (string, error) {
	if int64(len(content)) > maxDiffSourceBytes {
		return "", fmt.Errorf("untracked source %q exceeds %d-byte execution limit", path, maxDiffSourceBytes)
	}
	name := "a/" + path + " b/" + path
	dst := "b/" + path
	if strings.ContainsAny(path, " \t\n\\\"") {
		name = strconv.Quote("a/"+path) + " " + strconv.Quote("b/"+path)
		dst = strconv.Quote(dst)
	}
	limitError := fmt.Errorf("untracked patch for %q exceeds %d-byte execution limit", path, maxDiffPatchBytes)
	header := fmt.Sprintf("diff --git %s\nnew untracked file mode %s\n--- /dev/null\n+++ %s\n", name, mode, dst)
	if len(header) > maxDiffPatchBytes {
		return "", limitError
	}
	var body strings.Builder
	body.WriteString(header)
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		const prefix = "untracked content (base64): "
		encodedBytes := base64.StdEncoding.EncodedLen(len(content))
		if len(prefix)+encodedBytes+1 > maxDiffPatchBytes-body.Len() {
			return "", limitError
		}
		body.WriteString(prefix)
		body.WriteString(base64.StdEncoding.EncodeToString(content))
		body.WriteByte('\n')
		return body.String(), nil
	}
	if err := appendUntrackedTextBody(&body, content, limitError); err != nil {
		return "", err
	}
	return body.String(), nil
}

func appendUntrackedTextBody(body *strings.Builder, content []byte, limitError error) error {
	lineCount := bytes.Count(content, []byte{'\n'})
	if len(content) > 0 && content[len(content)-1] != '\n' {
		lineCount++
	}
	hunk := fmt.Sprintf("@@ -0,0 +1,%d @@\n", lineCount)
	if len(hunk) > maxDiffPatchBytes-body.Len() {
		return limitError
	}
	body.WriteString(hunk)
	for remaining := string(content); remaining != ""; {
		line, rest, _ := strings.Cut(remaining, "\n")
		if len(line)+2 > maxDiffPatchBytes-body.Len() {
			return limitError
		}
		body.WriteByte('+')
		body.WriteString(line)
		body.WriteByte('\n')
		remaining = rest
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		const marker = "\\ No newline at end of file\n"
		if len(marker) > maxDiffPatchBytes-body.Len() {
			return limitError
		}
		body.WriteString(marker)
	}
	return nil
}

func (p *Projector) validateDiffSnapshots() error {
	for _, check := range p.diffChecks {
		files, patch, err := captureDiff(p.repoRoot, check.paths)
		if err != nil {
			return fmt.Errorf("diff evidence changed before delivery: %w", err)
		}
		digest, _ := Digest(DiffBody{Question: check.question, Paths: check.paths, Files: files, Body: patch})
		if digest != check.digest {
			return fmt.Errorf("diff evidence changed before delivery; request fresh evidence")
		}
	}
	return nil
}

func gitLiteralOutputIn(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1")
	stdout := &boundedDiffOutput{limit: maxDiffPatchBytes}
	stderr := &boundedDiffOutput{limit: 8 * 1024}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.exceeded {
		return nil, fmt.Errorf("git %s output exceeds %d-byte execution limit", args[0], maxDiffPatchBytes)
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
