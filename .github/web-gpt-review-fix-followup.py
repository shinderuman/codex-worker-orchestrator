from pathlib import Path


def replace(path, old, new, count=1):
    p = Path(path)
    text = p.read_text()
    found = text.count(old)
    if found != count:
        raise SystemExit(f"{path}: expected {count} occurrence(s), found {found}: {old[:100]!r}")
    p.write_text(text.replace(old, new, count))


# Hooks run under deliberately constrained PATHs; locating the snapshot itself
# must use shell builtins, not dirname/cat from ambient PATH.
old_guard = '''hook_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
guard_path_file="$hook_dir/glm-parent-action.path"
if [ ! -f "$guard_path_file" ] || [ -L "$guard_path_file" ]; then
  printf '%s\\n' 'publication guard rejected: configured glm-parent-action path unavailable' >&2
  exit 1
fi
glm_parent_action=$(cat "$guard_path_file")
'''
new_guard = '''case "$0" in
  */*) hook_dir=${0%/*} ;;
  *) hook_dir=. ;;
esac
hook_dir=$(CDPATH='' cd -- "$hook_dir" && pwd)
guard_path_file="$hook_dir/glm-parent-action.path"
if [ ! -f "$guard_path_file" ] || [ -L "$guard_path_file" ]; then
  printf '%s\\n' 'publication guard rejected: configured glm-parent-action path unavailable' >&2
  exit 1
fi
if ! IFS= read -r glm_parent_action <"$guard_path_file"; then
  printf '%s\\n' 'publication guard rejected: configured glm-parent-action path unreadable' >&2
  exit 1
fi
'''
replace('.githooks/reference-transaction', old_guard, new_guard)
replace('.githooks/pre-push', old_guard, new_guard)

# Unit hook fixtures model the installer-owned sibling path file explicitly.
path = 'glm-worker/internal/parentactioncmd/publication_git_hook_smoke_test.go'
replace(path, '''\tbin, calls := publicationGuardStub(t, true)
\tif err := os.WriteFile(filepath.Join(repo, "code.txt"),''', '''\tbin, calls := publicationGuardStub(t, true)
\tbindPublicationHookGuard(t, hooks, bin)
\tif err := os.WriteFile(filepath.Join(repo, "code.txt"),''')
replace(path, '''\tbin, calls := publicationGuardStub(t, true)
\tcmd := exec.Command("git", "merge", "--ff-only", ahead)''', '''\tbin, calls := publicationGuardStub(t, true)
\tbindPublicationHookGuard(t, hooks, bin)
\tcmd := exec.Command("git", "merge", "--ff-only", ahead)''')
replace(path, '''\tbin, calls := publicationGuardStub(t, true)
\tt.Setenv("PATH", bin)''', '''\tbin, calls := publicationGuardStub(t, true)
\tbindPublicationHookGuard(t, hooks, bin)
\tt.Setenv("PATH", bin)''')
replace(path, '''\tbin, calls := publicationGuardStub(t, false)
\tif err := os.WriteFile(filepath.Join(repo, "blocked.txt"),''', '''\tbin, calls := publicationGuardStub(t, false)
\tbindPublicationHookGuard(t, hooks, bin)
\tif err := os.WriteFile(filepath.Join(repo, "blocked.txt"),''')
replace(path, '''func TestPublicationPrePushDelegatesAndFailsClosed(t *testing.T) {
\thook := continuationHookPath(t, ".githooks", "pre-push")
''', '''func TestPublicationPrePushDelegatesAndFailsClosed(t *testing.T) {
\thooks := t.TempDir()
\tcopyPublicationHook(t, "pre-push", hooks)
\thook := filepath.Join(hooks, "pre-push")
''')
replace(path, '''\t\tbin, calls := publicationGuardStub(t, true)
\t\tcmd := exec.Command("sh", hook, "origin", "unused")''', '''\t\tbin, calls := publicationGuardStub(t, true)
\t\tbindPublicationHookGuard(t, hooks, bin)
\t\tcmd := exec.Command("sh", hook, "origin", "unused")''')
replace(path, '''\t\tbin, _ := publicationGuardStub(t, false)
\t\tcmd := exec.Command("sh", hook, "origin", "unused")''', '''\t\tbin, _ := publicationGuardStub(t, false)
\t\tbindPublicationHookGuard(t, hooks, bin)
\t\tcmd := exec.Command("sh", hook, "origin", "unused")''')
replace(path, '''func publicationGuardStub(t *testing.T, allow bool) (string, string) {
''', '''func bindPublicationHookGuard(t *testing.T, hooks, bin string) {
\tt.Helper()
\tguard := filepath.Join(bin, "glm-parent-action")
\tif err := os.WriteFile(filepath.Join(hooks, "glm-parent-action.path"), []byte(guard+"\\n"), 0o600); err != nil {
\t\tt.Fatal(err)
\t}
}

func publicationGuardStub(t *testing.T, allow bool) (string, string) {
''')

# The corrected plain-stdout behavior intentionally preserves the complete Z.ai
# classification, so the older test expectation must preserve those fields too.
replace('glm-worker/internal/runner/stream_events_test.go', '''\t\t\twant: ProviderFailureClass{
\t\t\t\tKind:          ProviderFailureZaiFiveHour,
\t\t\t\tFiveHourLimit: ZaiFiveHourLimit{ResetAtCST: "2026-07-22 14:06:34", ResetAtRFC3339: "2026-07-22T14:06:34+08:00"},
\t\t\t},''', '''\t\t\twant: ProviderFailureClass{
\t\t\t\tKind:          ProviderFailureZaiFiveHour,
\t\t\t\tDetail:        "zai-code:1308",
\t\t\t\tBusinessCode:  "1308",
\t\t\t\tFiveHourLimit: ZaiFiveHourLimit{ResetAtCST: "2026-07-22 14:06:34", ResetAtRFC3339: "2026-07-22T14:06:34+08:00"},
\t\t\t},''')

# The finding is specifically that a post-execution user message must not make
# the shorter execution interval ambiguous. Finalization has its own existing
# projection semantics and is not changed by this finding.
replace('glm-worker/internal/app/bundle_parent_usage_interleaved_test.go', '''\tif report.Intervals.ParentFinalization.Tokens.Status != codexStatusAmbiguous || report.Intervals.ParentFinalization.Tokens.Reason != parentUsageReasonSameTurnInterleaved {
\t\tt.Fatalf("finalization interval = %#v", report.Intervals.ParentFinalization)
\t}
''', '')
