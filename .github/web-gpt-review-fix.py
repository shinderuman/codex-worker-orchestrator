from pathlib import Path


def replace(path, old, new, count=1):
    p = Path(path)
    text = p.read_text()
    found = text.count(old)
    if found != count:
        raise SystemExit(f"{path}: expected {count} occurrence(s), found {found}: {old[:100]!r}")
    p.write_text(text.replace(old, new, count))


# publication PreToolUse: fail closed on opaque wrappers and recognize commit -n.
replace(
    "glm-worker/internal/parentactioncmd/publication_pretool.go",
    '\tcase "coproc":\n\t\treturn index, true, true\n',
    '\tcase "coproc", "nohup", "setsid", "nice", "ionice", "stdbuf", "timeout", "xargs", "flock", "script":\n\t\treturn index, true, true\n',
)
replace(
    "glm-worker/internal/parentactioncmd/publication_pretool.go",
    '''func publicationGitNoVerify(argv []publicationShellWord) bool {
\tfor _, token := range argv {
\t\tif token.Value == "--no-verify" {
\t\t\treturn true
\t\t}
\t}
\treturn false
}
''',
    '''func publicationGitNoVerify(argv []publicationShellWord) bool {
\tsubcommand := publicationGitSubcommand(argv)
\tfor _, token := range argv {
\t\tif token.Value == "--no-verify" || subcommand == "commit" && token.Value == "-n" {
\t\t\treturn true
\t\t}
\t}
\treturn false
}

func publicationGitSubcommand(argv []publicationShellWord) string {
\tfor index := 0; index < len(argv); index++ {
\t\tvalue := argv[index].Value
\t\tif value == "--" {
\t\t\tif index+1 < len(argv) {
\t\t\t\treturn argv[index+1].Value
\t\t\t}
\t\t\treturn ""
\t\t}
\t\tswitch value {
\t\tcase "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env":
\t\t\tindex++
\t\t\tcontinue
\t\t}
\t\tif strings.HasPrefix(value, "-C") && value != "-C" || strings.HasPrefix(value, "-c") && value != "-c" ||
\t\t\tstrings.HasPrefix(value, "--git-dir=") || strings.HasPrefix(value, "--work-tree=") ||
\t\t\tstrings.HasPrefix(value, "--namespace=") || strings.HasPrefix(value, "--super-prefix=") || strings.HasPrefix(value, "--config-env=") {
\t\t\tcontinue
\t\t}
\t\tif strings.HasPrefix(value, "-") {
\t\t\tcontinue
\t\t}
\t\treturn value
\t}
\treturn ""
}
''',
)
replace(
    "glm-worker/internal/parentactioncmd/publication_pretool_test.go",
    '\t\t"git commit --no-verify -m bypass",\n',
    '\t\t"git commit --no-verify -m bypass",\n\t\t"git commit -n -m bypass",\n',
)
replace(
    "glm-worker/internal/parentactioncmd/publication_pretool_test.go",
    '\t\t"coproc worker git push origin main --no-verify",\n',
    '''\t\t"coproc worker git push origin main --no-verify",
\t\t"nohup git push origin main --no-verify",
\t\t"timeout 10 git commit --no-verify -m bypass",
\t\t"setsid git push origin main --no-verify",
\t\t"nice git push origin main --no-verify",
\t\t"ionice git push origin main --no-verify",
\t\t"stdbuf -o0 git push origin main --no-verify",
\t\t"xargs git push origin main --no-verify",
\t\t"flock /tmp/lock git push origin main --no-verify",
\t\t"script -c 'git push origin main --no-verify' /dev/null",
''',
)

# Runtime build identity must not claim a clean tree when git status failed.
replace(
    "install.sh",
    '''\tmodified=false
\tif [ -n "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all)" ]; then
\t\tmodified=true
\tfi
''',
    '''\tmodified=false
\tif ! status_output=$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all); then
\t\tprintf '%s\\n' 'failed to determine runtime build worktree state' >&2
\t\treturn 1
\tfi
\tif [ -n "$status_output" ]; then
\t\tmodified=true
\tfi
''',
)
replace(
    "install.sh",
    '''install_pull_hook() {
\tsh "$repo_root/scripts/manage-pull-hook.sh" install "$repo_root"
}
''',
    '''install_pull_hook() {
\tsh "$repo_root/scripts/manage-pull-hook.sh" install "$repo_root" "$bin_dir/glm-parent-action"
}
''',
)

# Hook installation keeps the previous snapshot until postconditions pass and
# binds publication hooks to the exact installed guard binary.
replace(
    "scripts/manage-pull-hook.sh",
    "mode=${1:-}\nrepo_root=${2:-}\nrequired_hooks='post-merge pre-commit reference-transaction pre-push'\n",
    "mode=${1:-}\nrepo_root=${2:-}\nglm_parent_action_path=${3:-}\nrequired_hooks='post-merge pre-commit reference-transaction pre-push'\n",
)
replace(
    "scripts/manage-pull-hook.sh",
    '''if [ -z "$repo_root" ]; then
\tprintf '%s\\n' 'repository path is required' >&2
\texit 2
fi

unset GIT_DIR''',
    '''if [ -z "$repo_root" ]; then
\tprintf '%s\\n' 'repository path is required' >&2
\texit 2
fi
if [ "$mode" = install ] && [ -z "$glm_parent_action_path" ]; then
\tglm_parent_action_path=$(command -v glm-parent-action || true)
fi
if [ "$mode" = install ]; then
\tcase "$glm_parent_action_path" in
\t/*) ;;
\t*)
\t\tprintf '%s\\n' 'git hook: absolute glm-parent-action path is required' >&2
\t\texit 1
\t\t;;
\tesac
\tif [ ! -x "$glm_parent_action_path" ]; then
\t\tprintf 'git hook: glm-parent-action is not executable: %s\\n' "$glm_parent_action_path" >&2
\t\texit 1
\tfi
fi

unset GIT_DIR''',
)
replace(
    "scripts/manage-pull-hook.sh",
    '''write_state() {
\tvalue=$1
\tstate_dir=${state_path%/*}
\tmkdir -p "$state_dir"
\ttmp_state="$state_path.tmp.$$"
\ttrap 'rm -f "$tmp_state"' EXIT HUP INT TERM
\tumask 077
\tprintf '%s\\n' "$value" >"$tmp_state"
\tmv "$tmp_state" "$state_path"
\ttrap - EXIT HUP INT TERM
}
''',
    '''write_state() {
\tvalue=$1
\tstate_dir=${state_path%/*}
\tmkdir -p "$state_dir"
\ttmp_state="$state_path.tmp.$$"
\tumask 077
\tif ! printf '%s\\n' "$value" >"$tmp_state"; then
\t\trm -f "$tmp_state"
\t\treturn 1
\tfi
\tif ! mv "$tmp_state" "$state_path"; then
\t\trm -f "$tmp_state"
\t\treturn 1
\tfi
}
''',
)
old_install = '''install_managed_hooks() {
\tstaging_hooks_path="$managed_hooks_path.stage.$$"
\tbackup_hooks_path="$managed_hooks_path.backup.$$"
\tcleanup_snapshot_work() {
\t\tif [ -e "$backup_hooks_path" ] || [ -L "$backup_hooks_path" ]; then
\t\t\tif [ ! -e "$managed_hooks_path" ] && [ ! -L "$managed_hooks_path" ]; then
\t\t\t\tmv "$backup_hooks_path" "$managed_hooks_path" || return 1
\t\t\telse
\t\t\t\trm -rf "$backup_hooks_path"
\t\t\tfi
\t\tfi
\t\trm -rf "$staging_hooks_path"
\t}
\ttrap 'cleanup_snapshot_work' EXIT HUP INT TERM
\trm -rf "$staging_hooks_path" "$backup_hooks_path"
\tmkdir -p "$staging_hooks_path"

\tfor hook in $required_hooks; do
\t\tstaged_hook="$staging_hooks_path/$hook"
\t\tif ! git -C "$repo_root" show "HEAD:.githooks/$hook" >"$staged_hook"; then
\t\t\tprintf 'git hook: committed source missing: .githooks/%s\\n' "$hook" >&2
\t\t\tcleanup_snapshot_work
\t\t\texit 1
\t\tfi
\t\tchmod 755 "$staged_hook"
\t\tif [ ! -s "$staged_hook" ] || [ ! -x "$staged_hook" ]; then
\t\t\tprintf 'git hook: staged snapshot is not executable and non-empty: .githooks/%s\\n' "$hook" >&2
\t\t\tcleanup_snapshot_work
\t\t\texit 1
\t\tfi
\tdone

\thad_active=0
\tif [ -e "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
\t\tif [ ! -d "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
\t\t\tprintf 'git hook: managed snapshot path is not an installer-owned directory: %s\\n' "$managed_hooks_path" >&2
\t\t\tcleanup_snapshot_work
\t\t\texit 1
\t\tfi
\t\tif ! mv "$managed_hooks_path" "$backup_hooks_path"; then
\t\t\tprintf '%s\\n' 'git hook: failed to stage existing managed snapshot for replacement' >&2
\t\t\tcleanup_snapshot_work
\t\t\texit 1
\t\tfi
\t\thad_active=1
\tfi

\tif ! mv "$staging_hooks_path" "$managed_hooks_path"; then
\t\tprintf '%s\\n' 'git hook: failed to activate validated managed snapshot' >&2
\t\tif [ "$had_active" -eq 1 ] && [ ! -e "$managed_hooks_path" ] && [ ! -L "$managed_hooks_path" ]; then
\t\t\tif ! mv "$backup_hooks_path" "$managed_hooks_path"; then
\t\t\t\tprintf 'git hook: failed to restore previous managed snapshot; backup retained at %s\\n' "$backup_hooks_path" >&2
\t\t\t\ttrap - EXIT HUP INT TERM
\t\t\t\texit 1
\t\t\tfi
\t\tfi
\t\tcleanup_snapshot_work
\t\texit 1
\tfi

\trm -rf "$backup_hooks_path"
\ttrap - EXIT HUP INT TERM
}
'''
new_install = '''staging_hooks_path=
backup_hooks_path=
had_active=0

cleanup_snapshot_work() {
\tif [ -e "$backup_hooks_path" ] || [ -L "$backup_hooks_path" ]; then
\t\trm -rf "$managed_hooks_path"
\t\tif ! mv "$backup_hooks_path" "$managed_hooks_path"; then
\t\t\tprintf 'git hook: failed to restore previous managed snapshot; backup retained at %s\\n' "$backup_hooks_path" >&2
\t\t\treturn 1
\t\tfi
\tfi
\trm -rf "$staging_hooks_path"
}

finalize_managed_hooks() {
\trm -rf "$staging_hooks_path" "$backup_hooks_path"
\ttrap - EXIT HUP INT TERM
}

install_managed_hooks() {
\tstaging_hooks_path="$managed_hooks_path.stage.$$"
\tbackup_hooks_path="$managed_hooks_path.backup.$$"
\thad_active=0
\ttrap 'cleanup_snapshot_work' EXIT HUP INT TERM
\trm -rf "$staging_hooks_path" "$backup_hooks_path"
\tmkdir -p "$staging_hooks_path"

\tprintf '%s\\n' "$glm_parent_action_path" >"$staging_hooks_path/glm-parent-action.path"
\tchmod 600 "$staging_hooks_path/glm-parent-action.path"

\tfor hook in $required_hooks; do
\t\tstaged_hook="$staging_hooks_path/$hook"
\t\tif ! git -C "$repo_root" show "HEAD:.githooks/$hook" >"$staged_hook"; then
\t\t\tprintf 'git hook: committed source missing: .githooks/%s\\n' "$hook" >&2
\t\t\texit 1
\t\tfi
\t\tchmod 755 "$staged_hook"
\t\tif [ ! -s "$staged_hook" ] || [ ! -x "$staged_hook" ]; then
\t\t\tprintf 'git hook: staged snapshot is not executable and non-empty: .githooks/%s\\n' "$hook" >&2
\t\t\texit 1
\t\tfi
\tdone

\tif [ -e "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
\t\tif [ ! -d "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
\t\t\tprintf 'git hook: managed snapshot path is not an installer-owned directory: %s\\n' "$managed_hooks_path" >&2
\t\t\texit 1
\t\tfi
\t\tif ! mv "$managed_hooks_path" "$backup_hooks_path"; then
\t\t\tprintf '%s\\n' 'git hook: failed to stage existing managed snapshot for replacement' >&2
\t\t\texit 1
\t\tfi
\t\thad_active=1
\tfi

\tif ! mv "$staging_hooks_path" "$managed_hooks_path"; then
\t\tprintf '%s\\n' 'git hook: failed to activate validated managed snapshot' >&2
\t\texit 1
\tfi
}
'''
replace("scripts/manage-pull-hook.sh", old_install, new_install)
replace(
    "scripts/manage-pull-hook.sh",
    '''\tfor hook in $required_hooks; do
\t\tactive_hook="$managed_hooks_path/$hook"
''',
    '''\tguard_path_file="$managed_hooks_path/glm-parent-action.path"
\tif [ ! -f "$guard_path_file" ] || [ -L "$guard_path_file" ] || [ "$(cat "$guard_path_file")" != "$glm_parent_action_path" ]; then
\t\tprintf '%s\\n' 'git hook: glm-parent-action path postcondition failed' >&2
\t\texit 1
\tfi
\tfor hook in $required_hooks; do
\t\tactive_hook="$managed_hooks_path/$hook"
''',
)
replace(
    "scripts/manage-pull-hook.sh",
    '''\t\tverify_managed_install "$managed_state"
\t\tprintf '%s\\n' 'git hook: refreshed installer-owned snapshot hooks'
''',
    '''\t\tverify_managed_install "$managed_state"
\t\tfinalize_managed_hooks
\t\tprintf '%s\\n' 'git hook: refreshed installer-owned snapshot hooks'
''',
)
replace(
    "scripts/manage-pull-hook.sh",
    '''\t\twrite_state "$managed_state"
\t\tverify_managed_install "$managed_state"
\t\tprintf '%s\\n' 'git hook: recovered interrupted installer-owned snapshot hooks activation'
''',
    '''\t\twrite_state "$managed_state"
\t\tverify_managed_install "$managed_state"
\t\tfinalize_managed_hooks
\t\tprintf '%s\\n' 'git hook: recovered interrupted installer-owned snapshot hooks activation'
''',
)
replace(
    "scripts/manage-pull-hook.sh",
    '''write_state "$managed_state"
verify_managed_install "$managed_state"
printf '%s\\n' 'git hook: enabled installer-owned snapshot hooks'
''',
    '''write_state "$managed_state"
verify_managed_install "$managed_state"
finalize_managed_hooks
printf '%s\\n' 'git hook: enabled installer-owned snapshot hooks'
''',
)

hook_guard = '''hook_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
guard_path_file="$hook_dir/glm-parent-action.path"
if [ ! -f "$guard_path_file" ] || [ -L "$guard_path_file" ]; then
  printf '%s\\n' 'publication guard rejected: configured glm-parent-action path unavailable' >&2
  exit 1
fi
glm_parent_action=$(cat "$guard_path_file")
case "$glm_parent_action" in
/*) ;;
*)
  printf '%s\\n' 'publication guard rejected: configured glm-parent-action path is not absolute' >&2
  exit 1
  ;;
esac
if [ ! -x "$glm_parent_action" ]; then
  printf '%s\\n' 'publication guard rejected: configured glm-parent-action is not executable' >&2
  exit 1
fi

'''
replace(
    ".githooks/reference-transaction",
    '''phase=${1:-}
if [ "$phase" != prepared ]; then
''',
    '''phase=${1:-}
''' + hook_guard + '''if [ "$phase" != prepared ]; then
''',
)
replace(
    ".githooks/reference-transaction",
    '''    if ! command -v glm-parent-action >/dev/null 2>&1; then
      printf '%s\\n' 'publication ref update rejected: glm-parent-action unavailable' >&2
      exit 1
    fi
    if ! glm-parent-action push-binding ref-guard''',
    '''    if ! "$glm_parent_action" push-binding ref-guard''',
)
replace(
    ".githooks/pre-push",
    '''remote_name=${1:-}
if [ -z "$remote_name" ]; then
''',
    '''remote_name=${1:-}
''' + hook_guard + '''if [ -z "$remote_name" ]; then
''',
)
replace(
    ".githooks/pre-push",
    '''    if ! command -v glm-parent-action >/dev/null 2>&1; then
      printf '%s\\n' 'publication push rejected: glm-parent-action unavailable' >&2
      exit 1
    fi
    if ! glm-parent-action push-binding push-guard \\
''',
    '''    if ! "$glm_parent_action" push-binding push-guard \\
''',
)

# Reference transaction authority protects every transition touching the candidate commit.
replace(
    "glm-worker/internal/parentactioncmd/publication_git_guard.go",
    '''\tif authority == "" {
\t\tif exactPromotion || exactRollback {
\t\t\treturn fmt.Errorf("publication ref update rejected: transaction authority missing")
\t\t}
\t\treturn nil
\t}
''',
    '''\tif authority == "" {
\t\tif oldOID == candidate.CommitOID || newOID == candidate.CommitOID {
\t\t\treturn fmt.Errorf("publication ref update rejected: transaction authority missing")
\t\t}
\t\treturn nil
\t}
''',
)
replace(
    "glm-worker/internal/parentactioncmd/publication_git_guard_test.go",
    "func TestPublicationRefGuardRejectsBoundNonCandidateMutation(t *testing.T) {\n",
    '''func TestPublicationRefGuardRejectsUnboundCandidateEndpointTransitions(t *testing.T) {
\tcfg, st := newInstallActionRepo(t)
\tif err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
\t\tt.Fatal(err)
\t}
\twritePushBindingFile(t, cfg.RepoRoot, "README.md", "guard candidate\\n")
\tpublicationGit(t, cfg.RepoRoot, "add", "README.md")
\tcandidate, failure := preparePublicationCandidate(cfg, st, "guard publication")
\tif failure != nil {
\t\tt.Fatalf("prepare failed: %#v", failure)
\t}
\tt.Setenv(publicationRefTransactionEnv, "")
\tbranchRef, _, headFailure := publicationPromotionHead(cfg.RepoRoot)
\tif headFailure != nil {
\t\tt.Fatalf("head = %#v", headFailure)
\t}
\tintermediateOID := strings.Repeat("f", 40)
\tif intermediateOID == candidate.CommitOID {
\t\tintermediateOID = strings.Repeat("e", 40)
\t}
\tfor _, transition := range [][2]string{{intermediateOID, candidate.CommitOID}, {candidate.CommitOID, intermediateOID}} {
\t\tif err := verifyPublicationRefUpdate(cfg, transition[0], transition[1], branchRef); err == nil || !strings.Contains(err.Error(), "transaction authority missing") {
\t\t\tt.Fatalf("unbound candidate endpoint transition was admitted: %s -> %s: %v", transition[0], transition[1], err)
\t\t}
\t}
}

func TestPublicationRefGuardRejectsBoundNonCandidateMutation(t *testing.T) {
''',
)

# Preserve terminal provider classifications seen only on plain stdout.
replace(
    "glm-worker/internal/runner/runner.go",
    '''func classifyPlainStdoutFailure(plain string) ProviderFailureClass {
\tclass := ClassifyProviderFailureText(plain)
\tif class.Kind == ProviderFailureZaiFiveHour {
\t\treturn ProviderFailureClass{Kind: class.Kind, FiveHourLimit: class.FiveHourLimit}
\t}
\tif class.Kind == ProviderFailureTransient {
\t\treturn class
\t}
\treturn ProviderFailureClass{}
}
''',
    '''func classifyPlainStdoutFailure(plain string) ProviderFailureClass {
\tclass := ClassifyProviderFailureText(plain)
\tswitch class.Kind {
\tcase ProviderFailureZaiFiveHour, ProviderFailureZaiLongQuota, ProviderFailureZaiActionRequired, ProviderFailureZaiUnknownSafeStop, ProviderFailureTransient:
\t\treturn class
\tdefault:
\t\treturn ProviderFailureClass{}
\t}
}
''',
)
replace(
    "glm-worker/internal/runner/transient_test.go",
    "func TestClassifyProviderFailureTextFallbackSignals(t *testing.T) {\n",
    '''func TestClassifyPlainStdoutFailurePreservesTerminalZaiClassification(t *testing.T) {
\tfor _, text := range []string{
\t\t`[1308][quota][2026-07-22 14:06:34]`,
\t\t`[1310][long quota]`,
\t\t`[1113][action required]`,
\t\t`[1999][unknown safe stop]`,
\t} {
\t\twant := ClassifyProviderFailureText(text)
\t\tgot := classifyPlainStdoutFailure(text)
\t\tif got != want {
\t\t\tt.Fatalf("plain stdout classification = %#v, want %#v", got, want)
\t\t}
\t}
}

func TestClassifyProviderFailureTextFallbackSignals(t *testing.T) {
''',
)

# Bind Z.ai reset timestamp to the record containing the recognized business code.
replace("glm-worker/internal/runner/zai_limit.go", '\t"regexp"\n\t"time"\n', '\t"regexp"\n\t"strings"\n\t"time"\n')
replace(
    "glm-worker/internal/runner/zai_limit.go",
    '''\tlimit := ZaiFiveHourLimit{}
\tmatch := zaiResetPattern.FindStringSubmatch(output)
''',
    '''\tlimit := ZaiFiveHourLimit{}
\trecord := zaiBusinessCodeRecord(output, code)
\tmatch := zaiResetPattern.FindStringSubmatch(record)
''',
)
replace(
    "glm-worker/internal/runner/zai_limit.go",
    "func FormatZaiResetAtCST(resetAtRFC3339 string) string {\n",
    '''func zaiBusinessCodeRecord(output, code string) string {
\tfor _, line := range strings.Split(output, "\\n") {
\t\tfor _, pattern := range []*regexp.Regexp{zaiJSONBusinessCodePattern, zaiBracketedBusinessCodePattern} {
\t\t\tfor _, match := range pattern.FindAllStringSubmatchIndex(line, -1) {
\t\t\t\tif len(match) >= 4 && line[match[2]:match[3]] == code {
\t\t\t\t\treturn line[match[1]:]
\t\t\t\t}
\t\t\t}
\t\t}
\t}
\treturn ""
}

func FormatZaiResetAtCST(resetAtRFC3339 string) string {
''',
)
replace(
    "glm-worker/internal/runner/zai_limit_test.go",
    "func TestDetectZaiFiveHourLimitKeepsInvalidResetUnschedulable(t *testing.T) {\n",
    '''func TestDetectZaiFiveHourLimitUsesResetFromMatchingProviderRecord(t *testing.T) {
\tcontent := "log 2026-07-22 01:02:03\\nAPI Error · [1308][quota][2026-07-22 14:06:34]"
\tlimit, ok := DetectZaiFiveHourLimitText(content)
\tif !ok {
\t\tt.Fatal("expected Z.ai 5h limit")
\t}
\tif limit.ResetAtRFC3339 != "2026-07-22T14:06:34+08:00" {
\t\tt.Fatalf("reset = %q", limit.ResetAtRFC3339)
\t}
\tsameLine := "2026-07-22 01:02:03 prefix [1308][quota][2026-07-22 14:06:34]"
\tlimit, ok = DetectZaiFiveHourLimitText(sameLine)
\tif !ok || limit.ResetAtRFC3339 != "2026-07-22T14:06:34+08:00" {
\t\tt.Fatalf("same-line reset = %#v, detected=%v", limit, ok)
\t}
}

func TestDetectZaiFiveHourLimitKeepsInvalidResetUnschedulable(t *testing.T) {
''',
)

# Empty Review findings is not a valid live finding section.
replace(
    "glm-worker/internal/taskcontract/dependencies.go",
    '''\tif reviewFindingsBody(lines, headingAt) == reviewFindingsNone {
\t\treturn ReviewFindings{}, fmt.Errorf("%s節は未解決findingがある場合だけ置き、findingなしは節自体を省略してください", ReviewFindingsHeading)
\t}
''',
    '''\tbody := reviewFindingsBody(lines, headingAt)
\tif body == "" || body == reviewFindingsNone {
\t\treturn ReviewFindings{}, fmt.Errorf("%s節は未解決findingがある場合だけ置き、findingなしは節自体を省略してください", ReviewFindingsHeading)
\t}
''',
)
replace(
    "glm-worker/internal/taskcontract/dependencies_test.go",
    '''\tif _, err := ParseReviewFindings([]byte("# Task\\n\\n## Review findings\\n\\nnone\\n")); err == nil {
\t\tt.Fatal("legacy Review findings none section was accepted")
\t}
''',
    '''\tif _, err := ParseReviewFindings([]byte("# Task\\n\\n## Review findings\\n\\nnone\\n")); err == nil {
\t\tt.Fatal("legacy Review findings none section was accepted")
\t}
\tif _, err := ParseReviewFindings([]byte("# Task\\n\\n## Review findings\\n\\n## Dependencies\\n\\nnone\\n")); err == nil {
\t\tt.Fatal("empty Review findings section was accepted")
\t}
''',
)

# Scope same-turn interleaving to the interval being projected.
replace("glm-worker/internal/app/bundle_analysis_resume_turn.go", "\tsameTurnInterleaved bool\n", "")
replace(
    "glm-worker/internal/app/bundle_analysis_resume_turn.go",
    "\townership.sameTurnInterleaved = analysisOwnershipHasSameTurnInterleavedUserMessage(scan, ownership, taskStart, collectionEnd)\n\treturn ownership\n",
    "\treturn ownership\n",
)
replace(
    "glm-worker/internal/app/bundle_parent_usage_ownership.go",
    '''\tif ownership.sameTurnInterleaved {
\t\treturn analysisUsageComparability{status: codexStatusAmbiguous, reason: parentUsageReasonSameTurnInterleaved}
\t}
''',
    '''\tif analysisOwnershipHasSameTurnInterleavedUserMessage(scan, ownership, start, end) {
\t\treturn analysisUsageComparability{status: codexStatusAmbiguous, reason: parentUsageReasonSameTurnInterleaved}
\t}
''',
)
replace(
    "glm-worker/internal/app/bundle_parent_usage_interleaved_test.go",
    "func analysisUserMessageLine(t *testing.T, timestamp time.Time) string {\n",
    '''func TestSameTurnInterleavingAfterExecutionDoesNotInvalidateExecutionInterval(t *testing.T) {
\ttask := newAnalysisTerminalTask(t)
\tturnStart := task.start.Add(-2 * time.Minute)
\tturnComplete := task.completeAt.Add(4 * time.Minute)
\tlines := []string{
\t\tanalysisTurnLine(t, turnStart, codexRolloutTaskStartedType, analysisOwningTurnID),
\t\tanalysisUserMessageLine(t, task.start.Add(-90*time.Second)),
\t\tparentUsageTokenCountLine(t, task.start.Add(-time.Second), 100, 50, 10, 5, 115),
\t\tparentUsageToolCallLine(t, task.start.Add(time.Minute), "task-tool"),
\t\tparentUsageTokenCountLine(t, task.completeAt.Add(-time.Second), 1000, 500, 160, 80, 1500),
\t\tanalysisUserMessageLine(t, task.completeAt.Add(time.Minute)),
\t\tanalysisTurnLine(t, turnComplete, codexRolloutTaskCompleteType, analysisOwningTurnID),
\t}
\twriteAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID, task.start.Add(-3*time.Hour), lines)
\treport := runParentUsageReport(t, task.cfg)
\tif report.Intervals.TaskExecution.Tokens.Status != analysisStatusAvailable || report.Intervals.TaskExecution.Activity.Status != analysisStatusCounted {
\t\tt.Fatalf("execution interval = %#v", report.Intervals.TaskExecution)
\t}
\tif report.Intervals.ParentFinalization.Tokens.Status != codexStatusAmbiguous || report.Intervals.ParentFinalization.Tokens.Reason != parentUsageReasonSameTurnInterleaved {
\t\tt.Fatalf("finalization interval = %#v", report.Intervals.ParentFinalization)
\t}
}

func analysisUserMessageLine(t *testing.T, timestamp time.Time) string {
''',
)

# One lifecycle status snapshot per execution-progress projection.
replace(
    "glm-worker/internal/workflow/execution_progress.go",
    '\tif reason := executionProgressPlanInconsistency(st, plan); reason != "" {\n',
    '\ttaskStatus := st.TaskStatus()\n\tif reason := executionProgressPlanInconsistency(st, plan, taskStatus); reason != "" {\n',
)
replace(
    "glm-worker/internal/workflow/execution_progress.go",
    '''\tprojection.Band = executionProgressBand(plan.CurrentIndex, len(plan.Milestones), phaseStage, st.TaskStatus())
\tif st.TaskStatus() == state.TaskStatusComplete {
''',
    '''\tprojection.Band = executionProgressBand(plan.CurrentIndex, len(plan.Milestones), phaseStage, taskStatus)
\tif taskStatus == state.TaskStatusComplete {
''',
)
replace(
    "glm-worker/internal/workflow/execution_progress.go",
    "func executionProgressPlanInconsistency(st *state.StateStore, plan *executionMilestonePlan) string {\n",
    "func executionProgressPlanInconsistency(st *state.StateStore, plan *executionMilestonePlan, taskStatus state.TaskStatus) string {\n",
)
replace(
    "glm-worker/internal/workflow/execution_progress.go",
    '\tif st.TaskStatus() == state.TaskStatusComplete && plan.CurrentIndex != len(plan.Milestones) {\n',
    '\tif taskStatus == state.TaskStatusComplete && plan.CurrentIndex != len(plan.Milestones) {\n',
)

# Forward-only lint understands multiline and quoted state_kind assignments.
replace(
    "glm-worker/internal/harnesslint/forward_only_shell_state.go",
    '\tshellStateKindPattern        = regexp.MustCompile(`^[\\t ]*"?\\$\\{?([A-Za-z_][A-Za-z0-9_]*)\\}?"?\\)[\\t ]*state_kind=([A-Za-z0-9_-]+)`)\n',
    '\tshellCaseVariablePattern    = regexp.MustCompile(`^[\\t ]*"?\\$\\{?([A-Za-z_][A-Za-z0-9_]*)\\}?"?\\)`)\n\tshellStateKindPattern       = regexp.MustCompile(`\\bstate_kind=(?:"([A-Za-z0-9_-]+)"|\'([A-Za-z0-9_-]+)\'|([A-Za-z0-9_-]+))`)\n',
)
replace(
    "glm-worker/internal/harnesslint/forward_only_shell_state.go",
    '''func shellStateKindForVariable(lines []string, variable string) string {
\tfor _, line := range lines {
\t\tmatch := shellStateKindPattern.FindStringSubmatch(strings.TrimSpace(line))
\t\tif len(match) == 3 && match[1] == variable {
\t\t\treturn match[2]
\t\t}
\t}
\treturn ""
}
''',
    '''func shellStateKindForVariable(lines []string, variable string) string {
\tfor index := 0; index < len(lines); index++ {
\t\tselector := shellCaseVariablePattern.FindStringSubmatch(strings.TrimSpace(lines[index]))
\t\tif len(selector) != 2 || selector[1] != variable {
\t\t\tcontinue
\t\t}
\t\tfor arm := index; arm < len(lines); arm++ {
\t\t\tmatch := shellStateKindPattern.FindStringSubmatch(lines[arm])
\t\t\tif len(match) == 4 {
\t\t\t\tfor _, kind := range match[1:] {
\t\t\t\t\tif kind != "" {
\t\t\t\t\t\treturn kind
\t\t\t\t\t}
\t\t\t\t}
\t\t\t}
\t\t\tif strings.Contains(lines[arm], ";;") {
\t\t\t\tbreak
\t\t\t}
\t\t}
\t}
\treturn ""
}
''',
)
marker = '''\t\t{
\t\t\tname: "preexisting hook layout adoption",
'''
addition = '''\t\t{
\t\t\tname: "multiline quoted old ownership state promotion",
\t\t\tpath: "scripts/manage-hooks.sh",
\t\t\tsource: `#!/bin/sh
old_state='version=1 baseline=absent value=.githooks'
current_state='version=2 baseline=absent value=/managed/hooks'
case "$(cat "$state_path")" in
"$old_state")
\tstate_kind="old-owned"
\t;;
"$current_state") state_kind=current-owned ;;
esac
case "$state_kind" in
old-owned)
\twrite_state "$current_state"
\t;;
esac
`,
\t\t},
'''
replace("glm-worker/internal/harnesslint/forward_only_shell_state_test.go", marker, addition + marker)

# Hook smoke: provide a stable installed guard binary and verify postcondition rollback.
replace(
    "tests/install_hook_ownership_smoke.sh",
    "real_git=$(command -v git)\nreal_mv=$(command -v mv)\n",
    '''real_git=$(command -v git)
real_mv=$(command -v mv)
real_cmp=$(command -v cmp)
mkdir -p "$tmp/bin"
guard_bin="$tmp/bin/glm-parent-action"
printf '#!/bin/sh\\nexit 0\\n' >"$guard_bin"
chmod 755 "$guard_bin"
PATH="$tmp/bin:$PATH"
export PATH
''',
)
replace(
    "tests/install_hook_ownership_smoke.sh",
    '''\tfor hook in post-merge pre-commit reference-transaction pre-push; do
\t\ttest -f "$managed/$hook"
''',
    '''\ttest "$(cat "$managed/glm-parent-action.path")" = "$guard_bin"
\tfor hook in post-merge pre-commit reference-transaction pre-push; do
\t\ttest -f "$managed/$hook"
''',
)
replace(
    "tests/install_hook_ownership_smoke.sh",
    "install_with_activation_failure() {\n",
    '''install_with_verification_failure() {
\trepo=$1
\tfakebin=$2
\tmkdir -p "$fakebin"
\tcat >"$fakebin/cmp" <<'EOF_FAKE_CMP'
#!/bin/sh
if [ "${FAIL_MANAGED_VERIFY:-0}" = 1 ]; then
\texit 75
fi
exec "$REAL_CMP" "$@"
EOF_FAKE_CMP
\tchmod 755 "$fakebin/cmp"
\tif PATH="$fakebin:$PATH" REAL_CMP="$real_cmp" FAIL_MANAGED_VERIFY=1 sh "$helper" install "$repo" >"$fakebin/install.stdout" 2>"$fakebin/install.stderr"; then
\t\tprintf '%s\\n' 'injected managed snapshot verification failure unexpectedly succeeded' >&2
\t\texit 1
\tfi
}

install_with_activation_failure() {
''',
)
replace(
    "tests/install_hook_ownership_smoke.sh",
    '''sh "$helper" install "$repo"
assert_managed_hooks "$repo"

repo="$tmp/detached-first"
''',
    '''sh "$helper" install "$repo"
assert_managed_hooks "$repo"

repo="$tmp/verification-rollback"
new_repo "$repo"
sh "$helper" install "$repo"
managed=$(managed_hooks_path "$repo")
before="$tmp/verification-before"
cp -R "$managed" "$before"
printf '#!/bin/sh\\nexit 66\\n' >"$repo/.githooks/pre-push"
chmod 755 "$repo/.githooks/pre-push"
git -C "$repo" add .githooks/pre-push
git -C "$repo" commit -qm 'change hook before verification failure'
install_with_verification_failure "$repo" "$tmp/fakecmp-verification"
for hook in post-merge pre-commit reference-transaction pre-push; do
\tcmp "$before/$hook" "$managed/$hook"
done
test "$(cat "$managed/glm-parent-action.path")" = "$guard_bin"

repo="$tmp/detached-first"
''',
)
replace(
    "tests/install_hook_ownership_smoke.sh",
    "grep -Fq 'enabled installer-owned snapshot hooks' \"$tmp/detached-first.stdout\"\n",
    '''grep -Fq 'enabled installer-owned snapshot hooks' "$tmp/detached-first.stdout"
managed=$(managed_hooks_path "$repo")
printf '%s %s %s\\n' "$(git -C "$repo" rev-parse HEAD)" "$(git -C "$repo" rev-parse HEAD)" refs/heads/main | PATH=/usr/bin:/bin "$managed/reference-transaction" prepared
''',
)
