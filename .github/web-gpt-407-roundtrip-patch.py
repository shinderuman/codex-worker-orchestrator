from pathlib import Path

source = Path("glm-worker/internal/codexcontext/project_agents.go")
text = source.read_text()
old = '''func removeProjectAgentsExclude(root string) error {
\tpath, err := gitExcludePath(root)
\tif err != nil {
\t\treturn err
\t}
\tcontent, err := os.ReadFile(path)
\tif errors.Is(err, os.ErrNotExist) {
\t\treturn nil
\t}
\tif err != nil {
\t\treturn fmt.Errorf("read local Git exclude: %w", err)
\t}
\tlines := strings.Split(string(content), "\\n")
\tout := make([]string, 0, len(lines))
\tfor index := 0; index < len(lines); index++ {
\t\tif strings.TrimSpace(lines[index]) == projectAgentsExcludeMarker && index+1 < len(lines) && strings.TrimSpace(lines[index+1]) == projectAgentsExcludePattern {
\t\t\tindex++
\t\t\tcontinue
\t\t}
\t\tout = append(out, lines[index])
\t}
\tnext := strings.Join(out, "\\n")
\tif next == string(content) {
\t\treturn nil
\t}
\tif err := os.WriteFile(path, []byte(next), 0o644); err != nil {
\t\treturn fmt.Errorf("write local Git exclude: %w", err)
\t}
\treturn nil
}
'''
new = '''func removeProjectAgentsExclude(root string) error {
\tpath, err := gitExcludePath(root)
\tif err != nil {
\t\treturn err
\t}
\tcontent, err := os.ReadFile(path)
\tif errors.Is(err, os.ErrNotExist) {
\t\treturn nil
\t}
\tif err != nil {
\t\treturn fmt.Errorf("read local Git exclude: %w", err)
\t}
\tblock := []byte(projectAgentsExcludeMarker + "\\n" + projectAgentsExcludePattern + "\\n")
\tnext := content
\tif bytes.HasSuffix(content, block) {
\t\tnext = content[:len(content)-len(block)]
\t\tif len(next) > 0 && !bytes.HasSuffix(next, []byte("\\n")) {
\t\t\treturn fmt.Errorf("managed project AGENTS exclude separator is inconsistent")
\t\t}
\t} else {
\t\tseparatorBlock := append([]byte("\\n"), block...)
\t\tif !bytes.HasSuffix(content, separatorBlock) {
\t\t\treturn nil
\t\t}
\t\tnext = content[:len(content)-len(separatorBlock)]
\t}
\tif err := os.WriteFile(path, next, 0o644); err != nil {
\t\treturn fmt.Errorf("write local Git exclude: %w", err)
\t}
\treturn nil
}
'''
if text.count(old) != 1:
    raise SystemExit(f"removeProjectAgentsExclude anchor count={text.count(old)}")
source.write_text(text.replace(old, new, 1))

tests = Path("glm-worker/internal/codexcontext/project_agents_test.go")
test_text = tests.read_text()
addition = r'''

func TestProjectAgentsExcludeRoundTripPreservesBytes(t *testing.T) {
	for _, original := range [][]byte{
		[]byte("# user rule"),
		[]byte("# user rule\n"),
	} {
		t.Run(strings.ReplaceAll(string(original), "\n", "newline"), func(t *testing.T) {
			repo := initTestRepo(t)
			excludePath := gitOutput(t, repo, "rev-parse", "--git-path", "info/exclude")
			if !filepath.IsAbs(excludePath) {
				excludePath = filepath.Join(repo, excludePath)
			}
			if err := os.WriteFile(excludePath, original, 0o644); err != nil {
				t.Fatal(err)
			}
			runAction(t, "enable", repo)
			runAction(t, "disable", repo)
			current, err := os.ReadFile(excludePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(current, original) {
				t.Fatalf("Git exclude changed: want %q got %q", original, current)
			}
		})
	}
}

func TestStatusFailsClosedWhenManagedProjectAgentsIsNotIgnored(t *testing.T) {
	repo := initTestRepo(t)
	runAction(t, "enable", repo)
	if err := removeProjectAgentsExclude(repo); err != nil {
		t.Fatal(err)
	}
	status := runAction(t, "status", repo)
	if status.Status != "conflict" {
		t.Fatalf("status = %+v", status)
	}
}
'''
if "func TestProjectAgentsExcludeRoundTripPreservesBytes" in test_text:
    raise SystemExit("round-trip test already exists")
tests.write_text(test_text.rstrip() + addition + "\n")

Path(".github/web-gpt-407-roundtrip-patch.py").unlink()
Path(".github/workflows/web-gpt-407-roundtrip-patch.yml").unlink()
