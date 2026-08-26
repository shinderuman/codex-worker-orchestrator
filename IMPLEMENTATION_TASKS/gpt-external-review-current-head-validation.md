# Task: GPT外部review proposalをcurrent HEADへ照合する

## Original instruction

````text
EXTERNAL_REVIEW_INTAKE
range: 6e2cdebfa148e60240b726199dd7a0aab63174a8..a75795589343ce86b63960f3662d02b50895d9bf
pr: https://github.com/shinderuman/codex-worker-orchestrator/pull/1
branch: gpt-review/6e2cdeb-a757955
proposal_head: 9e7dec61b352827c1406631bc8d29da6f3feff1a
この時点ではCodex自身によるコードレビュー・finding採否判断を行わない。
Draft PRのレビュー本文とreview branchの修正diffを取得し、全findingと修正proposalを間引かずGLMのreview/fix taskへlosslessに渡す。
GLMに現在HEADとの照合、finding成立性の検証、GPT修正案の検証・必要な適応、成立する問題の修正を行わせる。
GPT branchをblind mergeしない。
GLM処理完了後は既存のCodex最終review / acceptanceフローへ戻る。
````

## Amendments

none

## Resolved references

- Draft PR #1は取得時点でOPEN/Draft、base `main`、head `gpt-review/6e2cdeb-a757955`、proposal head `9e7dec61b352827c1406631bc8d29da6f3feff1a`である。
- reviewed rangeは`6e2cdebfa148e60240b726199dd7a0aab63174a8..a75795589343ce86b63960f3662d02b50895d9bf`であり、task実行時のcurrent HEADへ各findingとproposalを個別照合する。review branchをcurrent HEADの代替にしない。
- 取得元は`gh pr view 1 --json ...`と`gh pr diff 1`の成功結果である。以下のJSONはPR本文・全comment・全review・files metadataを、diffはproposal全体を取得時のまま保持する。

## External review payload

### gh PR metadata, body, comments, reviews, files

``````json
{"baseRefName":"main","baseRefOid":"e2765992c0ae596b6c566ea4189c4c283dd92287","body":"## Review provenance\n\n- reviewed range: `6e2cdebfa148e60240b726199dd7a0aab63174a8..a75795589343ce86b63960f3662d02b50895d9bf`\n- start: exclusive\n- end: inclusive and pinned before review\n- review branch: `gpt-review/6e2cdeb-a757955`\n- proposal head: `9e7dec61b352827c1406631bc8d29da6f3feff1a`\n\nThis is an external-review proposal. Do **not** blind-merge this branch into a later `main`; validate every finding and proposal against the current HEAD first.\n\n## Findings\n\n### F1 — HIGH: coalesce trusts a scheduler row whose RRULE disagrees with automation.toml\n\n`evaluateWakeCandidate` verified the TOML one-shot RRULE against the scheduler `next_run_at`, but did not compare the scheduler row's own `rrule` with the TOML RRULE. A stale or divergent scheduler row could therefore be accepted as a verified Codex wake and suppress creation of the dedicated GLM wake even though the two persisted scheduler representations disagree.\n\nImpact: the coalesce decision can claim a stronger postcondition than it actually verified. This directly contradicts the documented requirement that TOML and scheduler DB agree before an existing wake is trusted.\n\nProposal: require `db.Rrule == toml.Rrule` before coalescing; otherwise return `create_glm_wake` with a deterministic reason. Added a regression test with the same `next_run_at` but an `HOURLY` DB RRULE versus the expected one-shot `DAILY;COUNT=1` TOML RRULE.\n\n### F2 — HIGH: unvalidated automation IDs reach a raw sqlite3 SQL string\n\n`ReadDBRowSqlite3` interpolated `key` directly into `WHERE id = '%s'`. The older verify path validates keys with `keyPattern`, but the newly added coalesce path obtains the key from automation TOML/directory data and can call the DB reader without first enforcing that character set.\n\nImpact: a malformed local automation ID containing a quote can alter the SQL statement passed to the `sqlite3` CLI instead of merely making coalescing fail closed. Even without a malicious actor, malformed state reaches a boundary that should treat IDs as data, not SQL syntax.\n\nProposal: enforce the existing `keyPattern` at the DB boundary before looking up `sqlite3` or constructing SQL. Invalid keys return `ErrDBUnreadable`, causing coalesce to fall back to a dedicated GLM wake. Added a regression test that does not require sqlite3 to be installed.\n\n### F3 — HIGH: commentlint treats tab-indented terminators as valid for plain `<<EOF`\n\nThe shell scanner used `line == word || strings.TrimPrefix(line, \"\\t\") == word` for every heredoc. Shell only strips leading tabs from terminators for the `<<-` operator; ordinary `<<` requires the delimiter without that indentation. Conversely, `<<-` may strip multiple leading tabs, while the old code stripped at most one.\n\nImpact: commentlint can leave a heredoc body too early and classify payload lines as shell comments. In `--fix` mode that can delete or blank data that is semantically part of the heredoc payload. It can also fail to terminate a valid `<<-` heredoc with multiple leading tabs.\n\nProposal: carry `stripTabs` as part of the parsed heredoc descriptor, strip all leading tabs only for `<<-`, and keep plain `<<` exact. The existing multi-heredoc test now explicitly uses `<<-B`, and new tests cover both a tab-prefixed fake terminator for plain `<<` and a multi-tab valid terminator for `<<-`.\n\n### F4 — HIGH: commentlint `--fix` follows source symlinks and can rewrite files outside the repository\n\nThe Git-backed inventory includes tracked/untracked symlink paths. `Run` then used `os.ReadFile` and `os.WriteFile` on those paths; both follow symlinks. A source-looking symlink such as `linked.go -> /outside/file.go` can therefore make `commentlint --fix` rewrite the external target.\n\nImpact: a repository-wide cleanup tool can modify data outside the repository root, violating the fix boundary and potentially corrupting unrelated files.\n\nProposal: `Lstat` each classified source candidate and fail closed unless it is a regular file. Use the already-checked mode for pending writes. Added a regression test that stages a `.go` symlink, runs fix mode, and verifies the outside target is unchanged.\n\n## Finding → proposal mapping\n\n- F1: `glm-worker/internal/autoresume/coalesce.go`, `glm-worker/internal/autoresume/external_review_regression_test.go`\n- F2: `glm-worker/internal/autoresume/sqlite.go`, `glm-worker/internal/autoresume/external_review_regression_test.go`\n- F3: `glm-worker/internal/commentlint/commentlint.go`, `glm-worker/internal/commentlint/commentlint_test.go`\n- F4: `glm-worker/internal/commentlint/commentlint.go`, `glm-worker/internal/commentlint/commentlint_test.go`\n\n## Validation status\n\nRepository tests were **not executed** in the ChatGPT environment. The GitHub commit has no reported CI statuses at review time. A small standalone `/bin/sh` reproduction was used to confirm the relevant shell semantic: a tab-prefixed delimiter does not terminate plain `<<TAG` and remains payload until an exact `TAG` line.\n\nThe branch diff was re-read after all writes; net changes are limited to the five files listed above. The connector creates one commit per file mutation, so the proposal consists of multiple commits rather than a hand-squashed patch.\n\n## Residual risk / adoption contract\n\nGLM should re-evaluate all four findings against the current HEAD, run the repository's required targeted/full verification, and adapt the fixes if main has moved. The existence of this Draft PR is not evidence that the proposal passes tests or should be merged verbatim.","comments":[{"id":"IC_kwDOTyWmWs8AAAABQ7Q72w","author":{"login":"coderabbitai"},"authorAssociation":"NONE","body":"<!-- This is an auto-generated comment: summarize by coderabbit.ai -->\n<!-- This is an auto-generated comment: skip review by coderabbit.ai -->\n\n> [!IMPORTANT]\n> ## Draft PR not reviewed\n> \n> Draft PRs are not automatically reviewed by default.\n> \n> - [ ] <!-- {\"checkboxId\":\"e9bb8d72-00e8-4f67-9cb2-caf3b22574fe\"} --> Trigger a manual review\n> \n> To automatically review draft PRs, update your CodeRabbit configuration:\n> \n> ```yaml\n> reviews:\n>   auto_review:\n>     drafts: true\n> ```\n\n<!-- end of auto-generated comment: skip review by coderabbit.ai -->\n\n<!-- tips_start -->\n\n---\n\nThanks for using [CodeRabbit](https://coderabbit.ai?utm_source=oss&utm_medium=github&utm_campaign=shinderuman/codex-worker-orchestrator&utm_content=1)! It's free for OSS, and your support helps us grow. If you like it, consider giving us a shout-out.\n\n<details>\n<summary>❤️ Share</summary>\n\n- [X](https://twitter.com/intent/tweet?text=I%20just%20used%20%40coderabbitai%20for%20my%20code%20review%2C%20and%20it%27s%20fantastic%21%20It%27s%20free%20for%20OSS%20and%20offers%20a%20free%20trial%20for%20the%20proprietary%20code.%20Check%20it%20out%3A&url=https%3A//coderabbit.ai)\n- [Mastodon](https://mastodon.social/share?text=I%20just%20used%20%40coderabbitai%20for%20my%20code%20review%2C%20and%20it%27s%20fantastic%21%20It%27s%20free%20for%20OSS%20and%20offers%20a%20free%20trial%20for%20the%20proprietary%20code.%20Check%20it%20out%3A%20https%3A%2F%2Fcoderabbit.ai)\n- [Reddit](https://www.reddit.com/submit?title=Great%20tool%20for%20code%20review%20-%20CodeRabbit&text=I%20just%20used%20CodeRabbit%20for%20my%20code%20review%2C%20and%20it%27s%20fantastic%21%20It%27s%20free%20for%20OSS%20and%20offers%20a%20free%20trial%20for%20proprietary%20code.%20Check%20it%20out%3A%20https%3A//coderabbit.ai)\n- [LinkedIn](https://www.linkedin.com/sharing/share-offsite/?url=https%3A%2F%2Fcoderabbit.ai&mini=true&title=Great%20tool%20for%20code%20review%20-%20CodeRabbit&summary=I%20just%20used%20CodeRabbit%20for%20my%20code%20review%2C%20and%20it%27s%20fantastic%21%20It%27s%20free%20for%20OSS%20and%20offers%20a%20free%20trial%20for%20proprietary%20code)\n\n</details>\n\n\n<sub>Comment `@coderabbitai help` to get the list of available commands.</sub>\n\n<!-- tips_end -->","createdAt":"2026-08-26T20:40:47Z","includesCreatedEdit":false,"isMinimized":false,"minimizedReason":"","reactionGroups":[],"url":"https://github.com/shinderuman/codex-worker-orchestrator/pull/1#issuecomment-5430852571","viewerDidAuthor":false}],"files":[{"path":"glm-worker/internal/autoresume/coalesce.go","additions":4,"deletions":0,"changeType":"MODIFIED"},{"path":"glm-worker/internal/autoresume/external_review_regression_test.go","additions":56,"deletions":0,"changeType":"ADDED"},{"path":"glm-worker/internal/autoresume/sqlite.go","additions":3,"deletions":0,"changeType":"MODIFIED"},{"path":"glm-worker/internal/commentlint/commentlint.go","additions":27,"deletions":10,"changeType":"MODIFIED"},{"path":"glm-worker/internal/commentlint/commentlint_test.go","additions":44,"deletions":1,"changeType":"MODIFIED"}],"headRefName":"gpt-review/6e2cdeb-a757955","headRefOid":"9e7dec61b352827c1406631bc8d29da6f3feff1a","isDraft":true,"number":1,"reviews":[],"state":"OPEN","title":"[GPT external review] 6e2cdeb..a757955 findings and fix proposals","url":"https://github.com/shinderuman/codex-worker-orchestrator/pull/1"}

``````

### proposal diff

``````diff
diff --git a/glm-worker/internal/autoresume/coalesce.go b/glm-worker/internal/autoresume/coalesce.go
index a8529b4..445ec41 100644
--- a/glm-worker/internal/autoresume/coalesce.go
+++ b/glm-worker/internal/autoresume/coalesce.go
@@ -144,6 +144,10 @@ func evaluateWakeCandidate(toml AutomationTOML, params CoalesceParams, resumeAt
 		result.Reason = fmt.Sprintf("wake scheduler status is %q want ACTIVE", db.Status)
 		return result, nil
 	}
+	if db.Rrule != toml.Rrule {
+		result.Reason = "wake scheduler rrule does not match automation TOML"
+		return result, nil
+	}
 	if !db.HasNextRun {
 		result.Reason = "wake next_run_at is NULL"
 		return result, nil
diff --git a/glm-worker/internal/autoresume/external_review_regression_test.go b/glm-worker/internal/autoresume/external_review_regression_test.go
new file mode 100644
index 0000000..8b302b6
--- /dev/null
+++ b/glm-worker/internal/autoresume/external_review_regression_test.go
@@ -0,0 +1,56 @@
+package autoresume
+
+import (
+	"errors"
+	"strings"
+	"testing"
+	"time"
+)
+
+func TestEvaluateWakeCandidateRejectsSchedulerRruleMismatch(t *testing.T) {
+	wakeThread := "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
+	wakeID := codexWakeKeyPrefix + wakeThread
+	wakeAt, err := time.Parse(dtStartLayout, "20260826T152059")
+	if err != nil {
+		t.Fatal(err)
+	}
+	toml := AutomationTOML{
+		ID:             wakeID,
+		Name:           wakeID,
+		Status:         "ACTIVE",
+		Rrule:          "DTSTART:20260826T152059\nRRULE:FREQ=DAILY;COUNT=1",
+		TargetThreadID: wakeThread,
+	}
+	reader := func(string, string) (DBRow, error) {
+		return DBRow{
+			ID:         wakeID,
+			Status:     "ACTIVE",
+			Rrule:      "DTSTART:20260826T152059\nRRULE:FREQ=HOURLY;COUNT=1",
+			NextRunAt:  wakeAt.UnixMilli(),
+			HasNextRun: true,
+		}, nil
+	}
+	result, err := evaluateWakeCandidate(
+		toml,
+		CoalesceParams{DBPath: "unused"},
+		time.Date(2026, 8, 26, 15, 17, 55, 0, time.UTC),
+		reader,
+		CoalesceResult{Decision: DecisionCreateGLMWake},
+	)
+	if err != nil {
+		t.Fatal(err)
+	}
+	if result.Decision != DecisionCreateGLMWake || result.Reason != "wake scheduler rrule does not match automation TOML" {
+		t.Fatalf("result = %+v", result)
+	}
+}
+
+func TestReadDBRowSqlite3RejectsUnsafeAutomationKey(t *testing.T) {
+	_, err := ReadDBRowSqlite3("unused", "codex-5h-wake-x';SELECT 1;--")
+	if err == nil {
+		t.Fatal("unsafe automation key was accepted")
+	}
+	if !errors.Is(err, ErrDBUnreadable) || !strings.Contains(err.Error(), "invalid automation key format") {
+		t.Fatalf("error = %v", err)
+	}
+}
diff --git a/glm-worker/internal/autoresume/sqlite.go b/glm-worker/internal/autoresume/sqlite.go
index 5588495..8b9991a 100644
--- a/glm-worker/internal/autoresume/sqlite.go
+++ b/glm-worker/internal/autoresume/sqlite.go
@@ -10,6 +10,9 @@ import (
 )
 
 func ReadDBRowSqlite3(dbPath, key string) (DBRow, error) {
+	if !keyPattern.MatchString(key) {
+		return DBRow{}, fmt.Errorf("%w: invalid automation key format: %q", ErrDBUnreadable, key)
+	}
 	if _, err := exec.LookPath("sqlite3"); err != nil {
 		return DBRow{}, ErrSqlite3NotFound
 	}
diff --git a/glm-worker/internal/commentlint/commentlint.go b/glm-worker/internal/commentlint/commentlint.go
index 92e820d..697b2b6 100644
--- a/glm-worker/internal/commentlint/commentlint.go
+++ b/glm-worker/internal/commentlint/commentlint.go
@@ -46,6 +46,11 @@ type shellLine struct {
 	offset int
 }
 
+type heredocSpec struct {
+	word      string
+	stripTabs bool
+}
+
 func Check(root string) (Report, error) {
 	return Run(root, false)
 }
@@ -79,6 +84,13 @@ func Run(root string, fix bool) (Report, error) {
 			continue
 		}
 		absolute := filepath.Join(root, filepath.FromSlash(path))
+		info, err := os.Lstat(absolute)
+		if err != nil {
+			return Report{}, fmt.Errorf("stat %s: %w", path, err)
+		}
+		if !info.Mode().IsRegular() {
+			return Report{}, fmt.Errorf("source candidate %s is not a regular file", path)
+		}
 		data, err := os.ReadFile(absolute)
 		if err != nil {
 			return Report{}, fmt.Errorf("read %s: %w", path, err)
@@ -96,7 +108,7 @@ func Run(root string, fix bool) (Report, error) {
 		}
 		if fix && len(findings) > 0 {
 			updated := removeFindings(data, findings)
-			updates = append(updates, pendingUpdate{path: absolute, data: updated, mode: fileMode(absolute)})
+			updates = append(updates, pendingUpdate{path: absolute, data: updated, mode: info.Mode().Perm()})
 			report.Fixed += len(findings)
 			continue
 		}
@@ -245,7 +257,7 @@ func scanShell(path string, data []byte) []finding {
 
 func scanShellLines(path string, lines []shellLine) []finding {
 	var findings []finding
-	pending := []string{}
+	pending := []heredocSpec{}
 	bodyStart := 0
 	for index := 0; index < len(lines); index++ {
 		line := lines[index]
@@ -276,12 +288,15 @@ func scanShellLines(path string, lines []shellLine) []finding {
 	return findings
 }
 
-func heredocTerminated(word string, line string) bool {
-	return line == word || strings.TrimPrefix(line, "\t") == word
+func heredocTerminated(spec heredocSpec, line string) bool {
+	if spec.stripTabs {
+		line = strings.TrimLeft(line, "\t")
+	}
+	return line == spec.word
 }
 
-func heredocDelimiters(code string) []string {
-	var delimiters []string
+func heredocDelimiters(code string) []heredocSpec {
+	var delimiters []heredocSpec
 	single := false
 	double := false
 	escaped := false
@@ -307,7 +322,7 @@ func heredocDelimiters(code string) []string {
 			continue
 		}
 		delimiter, consumed := heredocDelimiter(code, index+2)
-		if delimiter != "" {
+		if delimiter.word != "" {
 			delimiters = append(delimiters, delimiter)
 		}
 		index = consumed - 1
@@ -315,9 +330,11 @@ func heredocDelimiters(code string) []string {
 	return delimiters
 }
 
-func heredocDelimiter(code string, start int) (string, int) {
+func heredocDelimiter(code string, start int) (heredocSpec, int) {
 	index := start
+	stripTabs := false
 	if index < len(code) && code[index] == '-' {
+		stripTabs = true
 		index++
 	}
 	for index < len(code) && (code[index] == ' ' || code[index] == '\t') {
@@ -333,13 +350,13 @@ func heredocDelimiter(code string, start int) (string, int) {
 		index++
 	}
 	if index == begin {
-		return "", start
+		return heredocSpec{}, start
 	}
 	end := index
 	if quote != 0 && index < len(code) && code[index] == quote {
 		index++
 	}
-	return code[begin:end], index
+	return heredocSpec{word: code[begin:end], stripTabs: stripTabs}, index
 }
 
 func heredocWordByte(value byte, first bool) bool {
diff --git a/glm-worker/internal/commentlint/commentlint_test.go b/glm-worker/internal/commentlint/commentlint_test.go
index f03d7bc..b72d55c 100644
--- a/glm-worker/internal/commentlint/commentlint_test.go
+++ b/glm-worker/internal/commentlint/commentlint_test.go
@@ -5,6 +5,7 @@ import (
 	"os/exec"
 	"path/filepath"
 	"reflect"
+	"strings"
 	"testing"
 )
 
@@ -80,7 +81,7 @@ func TestScanShellTreatsHereStringAsNonHeredoc(t *testing.T) {
 }
 
 func TestScanShellScansBodyOnlyAfterRealTerminator(t *testing.T) {
-	data := []byte("#!/bin/sh\ncat <<A <<B\n# body of A\nA\n# body of B\n\tB\n# tail\n")
+	data := []byte("#!/bin/sh\ncat <<A <<-B\n# body of A\nA\n# body of B\n\tB\n# tail\n")
 	findings := scanShell("a.sh", data)
 	lines := make([]int, 0, len(findings))
 	for _, item := range findings {
@@ -91,6 +92,28 @@ func TestScanShellScansBodyOnlyAfterRealTerminator(t *testing.T) {
 	}
 }
 
+func TestScanShellHonorsDashHeredocTabRules(t *testing.T) {
+	plain := []byte("#!/bin/sh\ncat <<EOF\n# payload\n\tEOF\n# still payload\nEOF\n# tail\n")
+	plainFindings := scanShell("plain.sh", plain)
+	plainLines := make([]int, 0, len(plainFindings))
+	for _, item := range plainFindings {
+		plainLines = append(plainLines, item.Line)
+	}
+	if !reflect.DeepEqual(plainLines, []int{7}) {
+		t.Fatalf("plain lines = %v", plainLines)
+	}
+
+	strip := []byte("#!/bin/sh\ncat <<-EOF\n# payload\n\t\tEOF\n# tail\n")
+	stripFindings := scanShell("strip.sh", strip)
+	stripLines := make([]int, 0, len(stripFindings))
+	for _, item := range stripFindings {
+		stripLines = append(stripLines, item.Line)
+	}
+	if !reflect.DeepEqual(stripLines, []int{5}) {
+		t.Fatalf("strip lines = %v", stripLines)
+	}
+}
+
 func TestScanHashAndGitignoreDistinguishData(t *testing.T) {
 	toml := []byte("a = \"# value\"\nb = 1 # prose\n")
 	if findings := scanHash("a.toml", toml); len(findings) != 1 || findings[0].Line != 2 {
@@ -173,6 +196,26 @@ func TestRunFixFailsClosedBeforeEditingUnclassifiedSource(t *testing.T) {
 	}
 }
 
+func TestRunFixRejectsSourceSymlinkWithoutFollowingIt(t *testing.T) {
+	root := t.TempDir()
+	runGit(t, root, "init")
+	external := filepath.Join(t.TempDir(), "external.go")
+	content := "package p\n// external prose\n"
+	writeFile(t, external, content)
+	link := filepath.Join(root, "linked.go")
+	if err := os.Symlink(external, link); err != nil {
+		t.Fatal(err)
+	}
+	runGit(t, root, "add", "linked.go")
+	_, err := Run(root, true)
+	if err == nil || !strings.Contains(err.Error(), "source candidate linked.go is not a regular file") {
+		t.Fatalf("error = %v", err)
+	}
+	if string(readFile(t, external)) != content {
+		t.Fatalf("external source changed = %q", readFile(t, external))
+	}
+}
+
 func TestRunWithoutGitUsesFilesystemInventory(t *testing.T) {
 	root := t.TempDir()
 	writeFile(t, filepath.Join(root, "a.go"), "package p\n// prose\n")

``````

## External feasibility

status: not-applicable

## Purpose

GPT external reviewの全findingとproposalをcurrent HEADの実コード・契約・testへGLMが独立照合し、成立する問題だけを必要に応じて適応修正する。

## Contract

- 親CodexはGLM処理前にfindingの採否判断やコードレビューを行わない
- GLMはPR本文・comment・review・proposal diffを間引かず読み、全findingをcurrent HEADで個別に成立/解消済み/false positiveへ分類する
- proposal実装を一次証拠として盲信せず、current HEADのproduction code・ACTIVE/完了task contract・既存testから妥当性と副作用を検証する
- 成立findingはcurrent HEADへ必要な形に適応して修正し、解消済み/false positiveは具体的なcode path/test evidenceを残して変更しない
- proposal branchのmerge、cherry-pick、blind patch適用は行わない
- GLM完了後は親Codexのsemantic final review、acceptance、通常quality gateへ戻す

## Must not

- findingやproposalを重要度・作業量・実装容易性で間引かない
- old reviewed rangeだけを見てcurrent HEADの後続変更を無視しない
- GPT proposalを正本としてcurrent source・contractより優先しない
- GLMにcommit/pushさせない
- 親CodexがGLM検証前に採否を先取りしない

## Acceptance criteria

- PR本文、全comment/review、5-file proposal diffの全内容がGLMへ渡る
- 全findingごとにcurrent HEADでの成立性、proposal妥当性、採用/適応/非採用理由が一次証拠付きで報告される
- 成立する問題だけがcurrent HEADへ修正され、proposal branchをblind mergeしていない
- 関連test・全必要gate・独立reviewを通し、親Codexが最終採否する

## Historical invariants

- review対象rangeの終端`a757955`以後にmainへ追加されたcommitをreview済みと仮定しない。
- 現在進行中のsandbox capability-aware quality gate taskを中断せず、その完了境界後に本taskへ移る。

## Dependencies

none

## Review findings

none

## Current boundary

NEXT。PR #2 external-review taskのpush後に親Codexが停止するため、本taskのGLM review/fix dispatchはまだ開始しない。
