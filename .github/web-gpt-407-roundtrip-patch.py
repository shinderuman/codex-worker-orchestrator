from pathlib import Path

source = Path("glm-worker/internal/codexcontext/project_agents.go")
text = source.read_text()

old_constants = '''const (
\tProjectAgentsOverrideRelativePath = "AGENTS.override.md"
\tprojectAgentsManagedMarker        = "<!-- managed-by: codex-worker-orchestrator glm-codex-context v1 -->"
\tprojectAgentsExcludeMarker        = "# codex-worker-orchestrator glm-codex-context agents v1"
\tprojectAgentsExcludePattern       = "/AGENTS.override.md"
)
'''
new_constants = '''const (
\tProjectAgentsOverrideRelativePath = "AGENTS.override.md"
\tprojectAgentsManagedMarker        = "<!-- managed-by: codex-worker-orchestrator glm-codex-context v1 -->"
\tprojectAgentsExcludeMarker        = "# codex-worker-orchestrator glm-codex-context agents v1"
\tprojectAgentsSeparatorMarker      = "# codex-worker-orchestrator glm-codex-context agents v1 separator-added"
\tprojectAgentsExcludePattern       = "/AGENTS.override.md"
)
'''
if text.count(old_constants) != 1:
    raise SystemExit(f"constant anchor count={text.count(old_constants)}")
text = text.replace(old_constants, new_constants, 1)

old_ensure = '''func ensureProjectAgentsExclude(root string) error {
\texcluded, err := projectAgentsIgnored(root)
\tif err != nil {
\t\treturn err
\t}
\tif excluded {
\t\treturn nil
\t}
\tpath, err := gitExcludePath(root)
\tif err != nil {
\t\treturn err
\t}
\tif err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
\t\treturn fmt.Errorf("create Git info directory: %w", err)
\t}
\texisting, err := os.ReadFile(path)
\tif err != nil && !errors.Is(err, os.ErrNotExist) {
\t\treturn fmt.Errorf("read local Git exclude: %w", err)
\t}
\tseparator := ""
\tif len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\\n")) {
\t\tseparator = "\\n"
\t}
\tfile, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
\tif err != nil {
\t\treturn fmt.Errorf("open local Git exclude: %w", err)
\t}
\tif _, err := fmt.Fprintf(file, "%s%s\\n%s\\n", separator, projectAgentsExcludeMarker, projectAgentsExcludePattern); err != nil {
\t\t_ = file.Close()
\t\treturn fmt.Errorf("write local Git exclude: %w", err)
\t}
\tif err := file.Close(); err != nil {
\t\treturn fmt.Errorf("close local Git exclude: %w", err)
\t}
\treturn nil
}
'''
new_ensure = '''func ensureProjectAgentsExclude(root string) error {
\texcluded, err := projectAgentsIgnored(root)
\tif err != nil {
\t\treturn err
\t}
\tif excluded {
\t\treturn nil
\t}
\tpath, err := gitExcludePath(root)
\tif err != nil {
\t\treturn err
\t}
\tif err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
\t\treturn fmt.Errorf("create Git info directory: %w", err)
\t}
\texisting, err := os.ReadFile(path)
\tif err != nil && !errors.Is(err, os.ErrNotExist) {
\t\treturn fmt.Errorf("read local Git exclude: %w", err)
\t}
\tseparator := ""
\tmarker := projectAgentsExcludeMarker
\tif len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\\n")) {
\t\tseparator = "\\n"
\t\tmarker = projectAgentsSeparatorMarker
\t}
\tfile, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
\tif err != nil {
\t\treturn fmt.Errorf("open local Git exclude: %w", err)
\t}
\tif _, err := fmt.Fprintf(file, "%s%s\\n%s\\n", separator, marker, projectAgentsExcludePattern); err != nil {
\t\t_ = file.Close()
\t\treturn fmt.Errorf("write local Git exclude: %w", err)
\t}
\tif err := file.Close(); err != nil {
\t\treturn fmt.Errorf("close local Git exclude: %w", err)
\t}
\treturn nil
}
'''
if text.count(old_ensure) != 1:
    raise SystemExit(f"ensureProjectAgentsExclude anchor count={text.count(old_ensure)}")
text = text.replace(old_ensure, new_ensure, 1)

old_remove = '''func removeProjectAgentsExclude(root string) error {
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
new_remove = '''func removeProjectAgentsExclude(root string) error {
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
\tseparatorBlock := []byte("\\n" + projectAgentsSeparatorMarker + "\\n" + projectAgentsExcludePattern + "\\n")
\tregularBlock := []byte(projectAgentsExcludeMarker + "\\n" + projectAgentsExcludePattern + "\\n")
\tnext := content
\tswitch {
\tcase bytes.HasSuffix(content, separatorBlock):
\t\tnext = content[:len(content)-len(separatorBlock)]
\tcase bytes.HasSuffix(content, regularBlock):
\t\tnext = content[:len(content)-len(regularBlock)]
\tdefault:
\t\treturn nil
\t}
\tif err := os.WriteFile(path, next, 0o644); err != nil {
\t\treturn fmt.Errorf("write local Git exclude: %w", err)
\t}
\treturn nil
}
'''
if text.count(old_remove) != 1:
    raise SystemExit(f"removeProjectAgentsExclude anchor count={text.count(old_remove)}")
source.write_text(text.replace(old_remove, new_remove, 1))

tests = Path("glm-worker/internal/codexcontext/project_agents_test.go")
test_text = tests.read_text()
addition = r'''

func TestProjectAgentsExcludeRoundTripPreservesBytes(t *testing.T) {
	for _, original := range [][]byte{
		[]byte("# user rule"),
		[]byte("# user rule\n"),
	} {
		name := "without-trailing-newline"
		if bytes.HasSuffix(original, []byte("\n")) {
			name = "with-trailing-newline"
		}
		t.Run(name, func(t *testing.T) {
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
