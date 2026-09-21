package harnesslint

import "testing"

func TestForwardOnlyCompatibilityRejectsShellStateCompatibility(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		source string
	}{
		{
			name: "old ownership state promotion",
			path: "scripts/manage-hooks.sh",
			source: `#!/bin/sh
old_state='version=1 baseline=absent value=.githooks'
current_state='version=2 baseline=absent value=/managed/hooks'
case "$(cat "$state_path")" in
"$old_state") state_kind=old-owned ;;
"$current_state") state_kind=current-owned ;;
esac
case "$state_kind" in
old-owned)
	write_state "$current_state"
	;;
esac
`,
		},
		{
			name: "preexisting hook layout adoption",
			path: "scripts/manage-hooks.sh",
			source: `#!/bin/sh
old_hooks_path=.githooks
managed_hooks_path=/managed/hooks
adopted_state="version=2 baseline=$old_hooks_path value=$managed_hooks_path"
write_state "$adopted_state"
`,
		},
		{
			name: "shell test guarantees state upgrade",
			path: "tests/install_hook_smoke.sh",
			source: `#!/bin/sh
state=/tmp/state
managed=/managed/hooks
printf '%s\n' 'version=1 baseline=absent value=.githooks' >"$state"
sh "$helper" install "$repo"
test "$(cat "$state")" = "version=2 baseline=absent value=$managed"
`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := fixtureRoot(t)
			writeFixture(t, root, testCase.path, testCase.source)
			requireRulePath(t, ruleViolations(t, root), forwardOnlyCompatibilityRule, testCase.path)
		})
	}
}

func TestForwardOnlyCompatibilityAllowsShellForwardOnlyHandling(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "scripts/manage-hooks.sh", `#!/bin/sh
old_state='version=1 baseline=absent value=.githooks'
current_state='version=2 baseline=absent value=/managed/hooks'
case "$(cat "$state_path")" in
"$old_state") state_kind=unsupported ;;
"$current_state") state_kind=current ;;
esac
case "$state_kind" in
unsupported)
	printf '%s\n' 'unsupported ownership state' >&2
	exit 1
	;;
current)
	write_state "$current_state"
	;;
esac
`)
	writeFixture(t, root, "tests/install_hook_smoke.sh", `#!/bin/sh
state=/tmp/state
printf '%s\n' 'version=1 baseline=absent value=.githooks' >"$state"
if sh "$helper" install "$repo"; then
	exit 1
fi
test "$(cat "$state")" = 'version=1 baseline=absent value=.githooks'
`)
	assertNoForwardOnlyViolations(t, ruleViolations(t, root))
}
