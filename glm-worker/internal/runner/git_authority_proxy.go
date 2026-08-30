package runner

import "strings"

const gitAuthorityProxyTemplate = `#!/bin/sh
real_git=__REAL_GIT__
attempt_log=__ATTEMPT_LOG__
guard_root=__REPO_ROOT__
temp_root=__TEMP_ROOT__
guard_root=$(cd "$guard_root" 2>/dev/null && pwd -P)
temp_root=$(cd "$temp_root" 2>/dev/null && pwd -P)
git_guard_record_attempt() {
  { printf '%s\n' "$1" >>"$attempt_log"; } 2>/dev/null || :
}
git_guard_deny() {
  reason=$1
  next_action=$2
  attempt=$3
  printf '{"error":{"kind":"containment_denial","owner":"git-authority-guard","reason":"%s","next_action":"%s"}}\n' "$reason" "$next_action" >&2
  git_guard_record_attempt "$attempt"
  exit 97
}
git_guard_normalize_path() {
  path=$1
  base=$2
  case "$path" in
    /*) candidate=$path ;;
    *) candidate=$base/$path ;;
  esac
  if [ -d "$candidate" ]; then
    (cd "$candidate" 2>/dev/null && pwd -P)
    return
  fi
  parent=$(dirname "$candidate")
  name=$(basename "$candidate")
  parent=$(cd "$parent" 2>/dev/null && pwd -P) || return 1
  printf '%s/%s\n' "$parent" "$name"
}
git_guard_effective_dir() {
  dir=$(pwd -P 2>/dev/null || pwd)
  expect=
  for arg do
    if [ -n "$expect" ]; then
      if [ "$expect" = C ]; then
        next=$(git_guard_normalize_path "$arg" "$dir") || return 1
        [ -d "$next" ] || return 1
        dir=$next
      fi
      expect=
      continue
    fi
    case "$arg" in
      -C) expect=C ;;
      -c|--git-dir|--work-tree|--namespace|--config-env) expect=skip ;;
      --git-dir=*|--work-tree=*|--namespace=*|--config-env=*) ;;
      --version|--help) break ;;
      -*) ;;
      *) break ;;
    esac
  done
  printf '%s\n' "$dir"
}
git_guard_command_name() {
  expect=0
  for arg do
    if [ "$expect" -eq 1 ]; then expect=0; continue; fi
    case "$arg" in
      -C|-c|--git-dir|--work-tree|--namespace|--config-env) expect=1 ;;
      --git-dir=*|--work-tree=*|--namespace=*|--config-env=*) ;;
      --version|--help) printf '%s\n' "$arg"; return ;;
      -*) ;;
      *) printf '%s\n' "$arg"; return ;;
    esac
  done
}
git_guard_path_scope() {
  target=$1
  case "$target" in
    "$guard_root"|"$guard_root"/*) printf '%s\n' protected ;;
    "$temp_root"|"$temp_root"/*) printf '%s\n' temp ;;
    *) printf '%s\n' other ;;
  esac
}
git_guard_explicit_scope() {
  dir=$(pwd -P 2>/dev/null || pwd)
  expect=
  git_dir=
  for arg do
    if [ -n "$expect" ]; then
      case "$expect" in
        C)
          next=$(git_guard_normalize_path "$arg" "$dir") || return 1
          [ -d "$next" ] || return 1
          dir=$next
          ;;
        gitdir)
          git_dir=$(git_guard_normalize_path "$arg" "$dir") || return 1
          ;;
        worktree)
          target=$(git_guard_normalize_path "$arg" "$dir") || return 1
          scope=$(git_guard_path_scope "$target")
          [ "$scope" = protected ] && { printf '%s\n' protected; return; }
          ;;
      esac
      expect=
      continue
    fi
    case "$arg" in
      -C) expect=C ;;
      --git-dir) expect=gitdir ;;
      --work-tree) expect=worktree ;;
      --git-dir=*) git_dir=$(git_guard_normalize_path "${arg#*=}" "$dir") || return 1 ;;
      --work-tree=*)
        target=$(git_guard_normalize_path "${arg#*=}" "$dir") || return 1
        scope=$(git_guard_path_scope "$target")
        [ "$scope" = protected ] && { printf '%s\n' protected; return; }
        ;;
      -c|--namespace|--config-env) expect=skip ;;
      --namespace=*|--config-env=*) ;;
      --version|--help) break ;;
      -*) ;;
      *) break ;;
    esac
  done
  if [ -n "$git_dir" ]; then git_guard_path_scope "$git_dir"; else printf '%s\n' none; fi
}
git_guard_init_target() {
  base=$1
  shift
  expect=0
  seen=0
  option_value=0
  for arg do
    if [ "$seen" -eq 0 ]; then
      if [ "$expect" -eq 1 ]; then expect=0; continue; fi
      case "$arg" in
        -C|-c|--git-dir|--work-tree|--namespace|--config-env) expect=1 ;;
        --git-dir=*|--work-tree=*|--namespace=*|--config-env=*) ;;
        -*) ;;
        init) seen=1 ;;
        *) return 1 ;;
      esac
      continue
    fi
    if [ "$option_value" -eq 1 ]; then option_value=0; continue; fi
    case "$arg" in
      --template|--separate-git-dir|-b|--initial-branch|--object-format|--ref-format) option_value=1 ;;
      --template=*|--separate-git-dir=*|--initial-branch=*|--object-format=*|--ref-format=*) ;;
      -*) ;;
      *) git_guard_normalize_path "$arg" "$base"; return ;;
    esac
  done
  printf '%s\n' "$base"
}
git_guard_scope() {
  explicit=$(git_guard_explicit_scope "$@") || { printf '%s\n' protected; return; }
  case "$explicit" in
    protected|temp|other) printf '%s\n' "$explicit"; return ;;
  esac
  dir=$(git_guard_effective_dir "$@") || { printf '%s\n' protected; return; }
  command_name=$(git_guard_command_name "$@")
  if [ "$command_name" = init ]; then
    target=$(git_guard_init_target "$dir" "$@") || { printf '%s\n' protected; return; }
    git_guard_path_scope "$target"
    return
  fi
  repo=$("$real_git" -C "$dir" rev-parse --show-toplevel 2>/dev/null) || repo=
  if [ -n "$repo" ]; then
    repo=$(cd "$repo" 2>/dev/null && pwd -P) || { printf '%s\n' protected; return; }
    git_guard_path_scope "$repo"
    return
  fi
  git_guard_path_scope "$dir"
}
git_guard_branch_read_only() {
  seen=0
  read_mode=0
  expect=0
  for arg do
    if [ "$seen" -eq 0 ]; then
      if [ "$expect" -eq 1 ]; then expect=0; continue; fi
      case "$arg" in
        -C|-c|--git-dir|--work-tree|--namespace|--config-env) expect=1 ;;
        --git-dir=*|--work-tree=*|--namespace=*|--config-env=*) ;;
        -*) ;;
        branch) seen=1 ;;
        *) return 1 ;;
      esac
      continue
    fi
    case "$arg" in
      -d|-D|-m|-M|-c|-C|-f|-u|--delete|--move|--copy|--force|--set-upstream|--set-upstream-to|--set-upstream-to=*|--unset-upstream|--edit-description|--track|--no-track|--recurse-submodules) return 1 ;;
      -l|--list|--contains|--contains=*|--no-contains|--no-contains=*|--merged|--merged=*|--no-merged|--no-merged=*|--show-current|-a|--all|-r|--remotes|-v|-vv|--verbose|--points-at|--points-at=*|--sort=*|--format=*|--column|--column=*|--no-column) read_mode=1 ;;
      -*) ;;
      *) if [ "$read_mode" -eq 0 ]; then return 1; fi ;;
    esac
  done
  return 0
}
git_guard_config_read_only() {
  seen=0
  expect=0
  option_value=0
  read_action=0
  positional=0
  for arg do
    if [ "$seen" -eq 0 ]; then
      if [ "$expect" -eq 1 ]; then expect=0; continue; fi
      case "$arg" in
        -C|-c|--git-dir|--work-tree|--namespace|--config-env) expect=1 ;;
        --git-dir=*|--work-tree=*|--namespace=*|--config-env=*) ;;
        -*) ;;
        config) seen=1 ;;
        *) return 1 ;;
      esac
      continue
    fi
    if [ "$option_value" -eq 1 ]; then option_value=0; continue; fi
    case "$arg" in
      --add|--replace-all|--unset|--unset-all|--rename-section|--remove-section|-e|--edit) return 1 ;;
      --get|--get-all|--get-regexp|--get-urlmatch|--list|-l|--get-color|--get-colorbool) read_action=1 ;;
      --file|-f|--blob|--type|--default) option_value=1 ;;
      --file=*|--blob=*|--type=*|--default=*|-z|--null|--name-only|--show-origin|--show-scope|--local|--global|--system|--worktree|--fixed-value|--includes|--no-includes) ;;
      -*) return 1 ;;
      get|list) read_action=1; positional=$((positional + 1)) ;;
      set|unset|rename-section|remove-section) return 1 ;;
      *) positional=$((positional + 1)) ;;
    esac
  done
  if [ "$read_action" -eq 1 ]; then return 0; fi
  [ "$positional" -le 1 ]
}
git_guard_remote_read_only() {
  seen=0
  expect=0
  action=
  for arg do
    if [ "$seen" -eq 0 ]; then
      if [ "$expect" -eq 1 ]; then expect=0; continue; fi
      case "$arg" in
        -C|-c|--git-dir|--work-tree|--namespace|--config-env) expect=1 ;;
        --git-dir=*|--work-tree=*|--namespace=*|--config-env=*) ;;
        -*) ;;
        remote) seen=1 ;;
        *) return 1 ;;
      esac
      continue
    fi
    case "$arg" in
      -v|--verbose|-n) ;;
      -*) ;;
      *) action=$arg; break ;;
    esac
  done
  case "$action" in
    ''|get-url|show) return 0 ;;
    *) return 1 ;;
  esac
}
git_guard_plain_read_tree() {
seen=0
expect=0
trees=0
for arg do
  if [ "$seen" -eq 0 ]; then
    if [ "$expect" -eq 1 ]; then expect=0; continue; fi
    case "$arg" in
      -C|-c|--git-dir|--work-tree|--namespace|--config-env) expect=1 ;;
      --git-dir=*|--work-tree=*|--namespace=*|--config-env=*) ;;
      -*) ;;
      read-tree) seen=1 ;;
      *) return 1 ;;
    esac
    continue
  fi
  case "$arg" in
    -*) return 1 ;;
    *) trees=$((trees + 1)) ;;
  esac
done
[ "$seen" -eq 1 ] && [ "$trees" -eq 1 ]
}
git_guard_temp_index_read_tree() {
git_guard_plain_read_tree "$@" || return 1
case "${GIT_INDEX_FILE:-}" in
  /*) ;;
  *) return 1 ;;
esac
if [ -e "$GIT_INDEX_FILE" ] || [ -L "$GIT_INDEX_FILE" ]; then return 1; fi
index=$(git_guard_normalize_path "$GIT_INDEX_FILE" /) || return 1
[ "$(git_guard_path_scope "$index")" = temp ]
}
git_guard_read_only() {
  command_name=$(git_guard_command_name "$@")
  case "$command_name" in
    status|diff|log|show|grep|ls-files|rev-parse|rev-list|merge-base|cat-file|for-each-ref|show-ref|describe|name-rev|shortlog|blame|ls-tree|check-ignore|check-attr|--version|--help) return 0 ;;
    branch) git_guard_branch_read_only "$@"; return ;;
    config) git_guard_config_read_only "$@"; return ;;
    remote) git_guard_remote_read_only "$@"; return ;;
    *) return 1 ;;
  esac
}
command_name=$(git_guard_command_name "$@")
if [ -z "$guard_root" ] || [ -z "$temp_root" ]; then
  git_guard_deny 'guard_scope_unavailable' 'stop_and_report_guard_state' '<guard-root>'
fi
scope=$(git_guard_scope "$@")
if [ "$scope" = temp ]; then
  GIT_ALLOW_PROTOCOL=file
  export GIT_ALLOW_PROTOCOL
  exec "$real_git" "$@"
fi
if [ "$scope" = protected ] && git_guard_temp_index_read_tree "$@"; then
  exec "$real_git" "$@"
fi
if git_guard_read_only "$@"; then
  exec "$real_git" "$@"
fi
if [ -z "$command_name" ]; then command_name='<missing-subcommand>'; fi
git_guard_deny 'git_mutation_blocked' 'continue_source_edits_or_read_only_git_parent_owns_git_mutation' "$command_name"
`

func gitAuthorityProxyScript(realGit, attemptLog, repoRoot, tempRoot string) string {
	return strings.NewReplacer(
		"__REAL_GIT__", shellSingleQuote(realGit),
		"__ATTEMPT_LOG__", shellSingleQuote(attemptLog),
		"__REPO_ROOT__", shellSingleQuote(repoRoot),
		"__TEMP_ROOT__", shellSingleQuote(tempRoot),
	).Replace(gitAuthorityProxyTemplate)
}

func gitAuthorityDenyTransportScript(attemptLog string) string {
	return "#!/bin/sh\n" +
		"printf '%s\\n' '{\"error\":{\"kind\":\"containment_denial\",\"owner\":\"git-authority-guard\",\"reason\":\"git_transport_blocked\",\"next_action\":\"do_not_retry_transport_parent_owns_git_transport\"}}' >&2\n" +
		"{ printf '%s\\n' 'transport' >>" + shellSingleQuote(attemptLog) + "; } 2>/dev/null || :\n" +
		"exit 97\n"
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
