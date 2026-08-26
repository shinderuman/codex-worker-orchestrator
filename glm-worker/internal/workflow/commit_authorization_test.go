package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestCommitAuthorizationSourceContractWiring(t *testing.T) {
	root := scenarioRepoRoot(t)

	readContractFile := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(b)
	}

	gitInstruction := readContractFile("codex/instructions/git.md")
	agents := readContractFile("codex/AGENTS.md")
	rules := readContractFile("IMPLEMENTATION_RULES.md")
	evalDoc := readContractFile("EVAL.md")

	cases := []struct {
		file string
		wire string
	}{
		{"codex/instructions/git.md", "## commit authorization source"},
		{"codex/instructions/git.md", "「明示的な依頼がない限り`git commit`しない」という安全規則は維持する"},
		{"codex/instructions/git.md", "明示的な依頼は文の配置場所ではなく、現在のtaskへ適用される明示的なユーザー意思の有無で判定する"},
		{"codex/instructions/git.md", "明示的な依頼の受理集合は、同一taskへ適用される会話上の明示的なcommit指示"},
		{"codex/instructions/git.md", "lossless requirement source(`Original instruction`・`Amendments`・`Resolved references`・ユーザー添付のlossless指示)"},
		{"codex/instructions/git.md", "最新メッセージ単体にcommit語がなくても既存task lifecycleを継続し、commit語の再要求だけでorchestrationを停止しない"},
		{"codex/instructions/git.md", "commit許可がどのsourceにも存在しない場合は従来どおりcommitしない"},
		{"codex/instructions/git.md", "過去にcommitした実績だけを将来のcommit許可へ拡張せず、commit語を含まない一般的な継続指示だけを無条件のcommit許可として扱わない"},
		{"codex/instructions/git.md", "対象task外の変更、別task・別repositoryへのcommitはこの許可に含まれない。GLM worker/reviewerにcommitさせない"},
		{"codex/instructions/git.md", "後述のrepository恒久許可だけが例外"},
		{"codex/instructions/git.md", "対象refへの通常fast-forwardのみを恒久許可した場合は、そのrepositoryの当該refに限りpush禁止の明示的な例外として扱い、commit単位で再許可を要求しない"},
		{"codex/instructions/git.md", "恒久許可refの受理集合は対象repositoryの親管理tracked instructionが唯一の正である"},
		{"codex/instructions/git.md", "この例外はforce/non-fast-forward、タグpush、列挙外ref、他repositoryへのremote書き込みへ拡張しない。GLM worker/reviewerによるpushは常に禁止する"},

		{"codex/AGENTS.md", "`git commit`はユーザーが明示的に依頼した場合だけ行う"},
		{"codex/AGENTS.md", "ユーザーがrepositoryの親管理tracked instructionで列挙refへの通常fast-forwardとして恒久許可した場合だけ例外"},
		{"codex/AGENTS.md", "明示的な依頼には同一taskへの会話上の明示指示と現在のtaskのlossless requirement source"},
		{"codex/AGENTS.md", "lossless requirement source(`Original instruction`・`Amendments`・`Resolved references`・ユーザー添付指示)"},
		{"codex/AGENTS.md", "最新メッセージ単体のcommit語の有無だけでは判定しない"},
	}
	contents := make(map[string]string, 4)
	for _, c := range cases {
		if _, ok := contents[c.file]; !ok {
			contents[c.file] = readContractFile(c.file)
		}
		if !strings.Contains(contents[c.file], c.wire) {
			t.Errorf("%s lacks commit authorization source wiring: %q", c.file, c.wire)
		}
	}

	evalSection := markdownSectionOf(t, evalDoc, "## commit/push authorization source認識contract")
	for _, wire := range []string{
		"Task 009完了時のcommit承認false negativeを一次証拠で再現可能に整理する",
		"「ユーザーによる明示的なcommit依頼が確認できない」と2回停止させた",
		"過去のcommit実績だけを将来許可へ拡張しない",
		"恒久許可refの受理集合は`IMPLEMENTATION_RULES.md`の`## commit / install`節が唯一の正であり",
		"TestCommitAuthorizationSourceContractWiring",
		"親Codex behavioral Evalは未実行の固定Eval caseとする",
		"live model呼出しを要するためユーザーの明示指示後だけ実行し",
		"実行承認境界は親Codex model層の規則解釈でありrepoから機械強制できない",
		"corpusへ`commit-authorization-*`scenarioを追加しない",
	} {
		if !strings.Contains(evalSection, wire) {
			t.Errorf("EVAL.md commit authorization section lacks wiring: %q", wire)
		}
	}

	rulesRefs := sortedRemoteRefs(markdownSectionOf(t, rules, "## commit / install"))
	gitRefs := sortedRemoteRefs(markdownSectionOf(t, gitInstruction, "## commit authorization source"))
	evalRefs := sortedRemoteRefs(evalSection)
	if len(rulesRefs) == 0 {
		t.Fatalf("IMPLEMENTATION_RULES.md commit/install section enumerates no remote refs")
	}
	if strings.Join(rulesRefs, ",") != strings.Join(gitRefs, ",") {
		t.Errorf("git.md commit authorization refs %v must equal IMPLEMENTATION_RULES.md permitted refs %v", gitRefs, rulesRefs)
	}
	if strings.Join(rulesRefs, ",") != strings.Join(evalRefs, ",") {
		t.Errorf("EVAL.md commit authorization refs %v must equal IMPLEMENTATION_RULES.md permitted refs %v", evalRefs, rulesRefs)
	}

	agentsSection := markdownSectionOf(t, agents, "## 3. Git絶対規則")
	if refs := sortedRemoteRefs(agentsSection); len(refs) != 0 {
		t.Errorf("codex/AGENTS.md section 3 must delegate ref enumeration to repository-tracked instruction instead of enumerating %v", refs)
	}

	repoAgents := readContractFile("AGENTS.md")
	if refs := sortedRemoteRefs(repoAgents); len(refs) != 0 {
		t.Errorf("root AGENTS.md must not define its own remote ref permission set %v", refs)
	}
	if strings.Contains(repoAgents, "git commit") || strings.Contains(repoAgents, "git push") {
		t.Errorf("root AGENTS.md must not define an independent git commit/push authorization set")
	}

	for _, promptFile := range []string{"codex/glm-worker/prompts/WORKER.md", "codex/glm-worker/prompts/REVIEWER.md"} {
		prompt := readContractFile(promptFile)
		for _, keyword := range []string{"authorization source", "恒久許可", "commit authorization"} {
			if strings.Contains(prompt, keyword) {
				t.Errorf("%s must not duplicate the commit authorization source contract (%s)", promptFile, keyword)
			}
		}
	}

	sc, _ := loadCorpus(t)
	for _, s := range sc.Scenarios {
		if strings.HasPrefix(s.ID, "commit-authorization-") {
			t.Errorf("scenario %s must not duplicate the parent behavioral eval into the corpus", s.ID)
		}
	}
}

var commitAuthorizationRefPattern = regexp.MustCompile(`refs/heads/[A-Za-z0-9._/-]+`)

func sortedRemoteRefs(section string) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, m := range commitAuthorizationRefPattern.FindAllString(section, -1) {
		if !seen[m] {
			seen[m] = true
			refs = append(refs, m)
		}
	}
	sort.Strings(refs)
	return refs
}

func markdownSectionOf(t *testing.T, doc string, header string) string {
	t.Helper()
	start := strings.Index(doc, header)
	if start < 0 {
		t.Fatalf("document lacks section header %q", header)
	}
	rest := doc[start+len(header):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}
