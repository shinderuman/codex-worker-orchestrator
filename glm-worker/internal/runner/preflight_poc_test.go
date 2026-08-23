package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// このfileはClaude CLI compatibility preflightのPoCである。productionの
// startup・workflow・state semanticsへは接続しておらず、採用時のみ独立taskで
// 本体packageへ昇格する。preflightが実行するのは `claude --version` と
// `claude --help` の2呼出だけ(AI call 0件・task/session/checkpoint状態には触れない)。

const (
	preflightBinaryAbsent         = "binary-absent"
	preflightBinaryNotExecutable  = "binary-not-executable"
	preflightVersionFailed        = "version-failed"
	preflightHelpFailed           = "help-failed"
	preflightFeatureMissing       = "feature-missing"
	preflightOutputFormatsMissing = "output-format-vocabulary-missing"
	preflightFakeUnexpectedArgsEc = 87
)

// claudePreflightFeatureはinternal/runnerがClaude CLI argvへ渡すflag 1件と、
// それを使う呼出経路。surfacesは run(実task)・probe(provider recovery)の組合せ。
type claudePreflightFeature struct {
	flag     string
	surfaces string
}

// claudePreflightFeaturesはproduction argvに現れるflagの全集合。runner.go/probe.goへ
// flagを追加した場合はこのinventoryへ同じflagを足す。
// TestClaudePreflightInventoryMatchesRunnerSourceFlagsが両方向の更新漏れを検出する。
var claudePreflightFeatures = []claudePreflightFeature{
	{flag: "-p", surfaces: "run+probe"},
	{flag: "--safe-mode", surfaces: "run+probe"},
	{flag: "--setting-sources", surfaces: "run+probe"},
	{flag: "--resume", surfaces: "run"},
	{flag: "--session-id", surfaces: "run"},
	{flag: "--name", surfaces: "run"},
	{flag: "--model", surfaces: "run+probe"},
	{flag: "--effort", surfaces: "run+probe"},
	{flag: "--autocompact", surfaces: "run"},
	{flag: "--output-format", surfaces: "run+probe"},
	{flag: "--verbose", surfaces: "run"},
	{flag: "--dangerously-skip-permissions", surfaces: "run+probe"},
	{flag: "--strict-mcp-config", surfaces: "run+probe"},
	{flag: "--mcp-config", surfaces: "run+probe"},
	{flag: "--disable-slash-commands", surfaces: "run+probe"},
	{flag: "--settings", surfaces: "run+probe"},
	{flag: "--json-schema", surfaces: "run"},
	{flag: "--disallowedTools", surfaces: "run"},
	{flag: "--append-system-prompt-file", surfaces: "run"},
	{flag: "--no-session-persistence", surfaces: "probe"},
	{flag: "--tools", surfaces: "probe"},
}

type claudePreflightFailure struct {
	category string
	feature  string
	detail   string
}

type claudePreflightReport struct {
	version  string
	ok       bool
	failures []claudePreflightFailure
}

// claudeCompatPreflightはClaude CLI binaryのflag互換性をmodel callなしで確認する。
// binary不在・非実行可能・version/help失敗・必須feature欠落はすべてfail closedで
// 呼出元へ返す。help本文だけでは検証できないstream/result semantics・session/resume
// 契約は対象外(非保証)であり、この関数はそれらを保証済みにしない。
func claudeCompatPreflight(bin string) claudePreflightReport {
	report := claudePreflightReport{}
	resolved := bin
	if !strings.ContainsRune(bin, os.PathSeparator) {
		lookedUp, err := exec.LookPath(bin)
		if err != nil {
			report.failures = append(report.failures, claudePreflightFailure{
				category: preflightBinaryAbsent,
				detail:   err.Error(),
			})
			return report
		}
		resolved = lookedUp
	}
	info, err := os.Stat(resolved)
	if err != nil {
		report.failures = append(report.failures, claudePreflightFailure{
			category: preflightBinaryAbsent,
			detail:   err.Error(),
		})
		return report
	}
	if info.Mode().Perm()&0o111 == 0 {
		report.failures = append(report.failures, claudePreflightFailure{
			category: preflightBinaryNotExecutable,
			detail:   resolved,
		})
		return report
	}
	versionBytes, err := exec.Command(resolved, "--version").Output()
	if err != nil {
		report.failures = append(report.failures, claudePreflightFailure{
			category: preflightVersionFailed,
			detail:   err.Error(),
		})
		return report
	}
	report.version = strings.TrimSpace(string(versionBytes))
	helpBytes, err := exec.Command(resolved, "--help").Output()
	if err != nil {
		report.failures = append(report.failures, claudePreflightFailure{
			category: preflightHelpFailed,
			detail:   err.Error(),
		})
		return report
	}
	help := string(helpBytes)
	for _, feature := range claudePreflightFeatures {
		if claudeHelpDocumentsFlag(help, feature.flag) {
			continue
		}
		report.failures = append(report.failures, claudePreflightFailure{
			category: preflightFeatureMissing,
			feature:  feature.flag,
			detail:   fmt.Sprintf("claude --helpが%s(%s)を案内していません", feature.flag, feature.surfaces),
		})
	}
	if !claudeHelpDocumentsOutputFormats(help) {
		report.failures = append(report.failures, claudePreflightFailure{
			category: preflightOutputFormatsMissing,
			detail:   `--output-formatのchoices説明に"json"と"stream-json"がありません`,
		})
	}
	report.ok = len(report.failures) == 0
	return report
}

// claudeHelpDocumentsFlagはhelp本文がflagをoption宣言として案内しているかを判定する。
// 宣言行(行頭の浅いindentで始まるoption token列)への照合に限定し、他optionの説明文へ
// の言及は案内とみなさない。--mcp-configの宣言を消しても--strict-mcp-configの説明文が
// 言及し続けるような形のfalse acceptを防ぐためである。bracket圧縮表記
// `--append-system-prompt[-file]` は宣言行を持たない正式なoption名表記のため、
// こちらだけ本文全域で照合する。
func claudeHelpDocumentsFlag(help string, flag string) bool {
	if claudeHelpDeclarationPattern(flag).MatchString(help) {
		return true
	}
	_, documented := claudeBracketExpandedFlags(help)[flag]
	return documented
}

// claudeHelpDeclarationPatternはflagのoption宣言行(行頭の浅いindent、直前のcomma区切り
// alias列、flag token)に一致させる。`-r, --resume [value]` のようなshort alias併記と
// `--disallowedTools, --disallowed-tools` のようなlong alias併記を捕捉する。説明文の
// wrap行は深いindentで始まるため宣言位置と区別できる。
func claudeHelpDeclarationPattern(flag string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?m)^([ \t]{0,6})((?:(?:--|-)[A-Za-z0-9][A-Za-z0-9-]*,[ \t]+)*)` +
			regexp.QuoteMeta(flag) + `($|[ \t,<\[\]=])`)
}

// claudeBracketExpandedFlagsはClaude Code 2.1.226の--bare説明にある
// `--append-system-prompt[-file]` 形式のbracket圧縮表記を展開したtoken集合。
// 圧縮表記はbase flagとsuffix拡張flagの2 option名の正式な省略表記のため、展開して
// 照合しないと現行CLIが持つ--append-system-prompt-fileを誤拒否する。
func claudeBracketExpandedFlags(help string) map[string]bool {
	expanded := map[string]bool{}
	for _, token := range regexp.MustCompile(`--[A-Za-z0-9-]+\[-[A-Za-z0-9-]+\]`).FindAllString(help, -1) {
		base, suffix, _ := strings.Cut(token, "[")
		expanded[base] = true
		expanded[base+strings.TrimSuffix(suffix, "]")] = true
	}
	return expanded
}

// claudeHelpDocumentsOutputFormatsは--output-format宣言blockの説明が実行protocol依存の
// "json"(Probe経路)と"stream-json"(Run経路)を含むかを検査する。blockの外にある
// --input-format等の同じ語彙は数えない。
func claudeHelpDocumentsOutputFormats(help string) bool {
	block := claudeHelpOutputFormatBlock(help)
	return strings.Contains(block, `"json"`) && strings.Contains(block, `"stream-json"`)
}

var claudeHelpDeclarationLinePattern = regexp.MustCompile(`^[ \t]{0,6}-`)

// claudeHelpOutputFormatBlockは--output-format宣言行から次の宣言行までのhelp範囲を返す。
// 宣言行がなければ空を返し、呼出元はfail closedとして扱う。
func claudeHelpOutputFormatBlock(help string) string {
	lines := strings.Split(help, "\n")
	start := -1
	for i, line := range lines {
		if claudeHelpDeclarationLinePattern.MatchString(line) && strings.HasPrefix(strings.TrimSpace(line), "--output-format") {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	block := []string{lines[start]}
	for i := start + 1; i < len(lines); i++ {
		if claudeHelpDeclarationLinePattern.MatchString(lines[i]) {
			break
		}
		block = append(block, lines[i])
	}
	return strings.Join(block, "\n")
}

// claudeHelpWithoutFlagはfeature欠落fixtureとして、helpからflagの宣言だけを取り除く。
// 説明文への言及は残す(宣言消失・prose言及残存が現実のdrift形態のため)。
// 続けてbracket圧縮表記はbase表記へ置き換える。
func claudeHelpWithoutFlag(help string, flag string) string {
	reduced := claudeHelpDeclarationPattern(flag).ReplaceAllString(help, "${1}${2}--removed-feature${3}")
	for _, token := range regexp.MustCompile(`--[A-Za-z0-9-]+\[-[A-Za-z0-9-]+\]`).FindAllString(reduced, -1) {
		base, suffix, _ := strings.Cut(token, "[")
		if base+strings.TrimSuffix(suffix, "]") == flag {
			reduced = strings.ReplaceAll(reduced, token, base)
		}
	}
	return reduced
}

// writeFakeClaudePreflightBinaryは--version/--helpだけに応答するfake claude binaryを
// 作る。起動されるたびに引数を行単位でrecord fileへ残し、想定外の起動引数は
// exit 87で失敗させる(preflightがmodel呼出を起こしていないことの機械検証)。
func writeFakeClaudePreflightBinary(t *testing.T, helpText string, versionExitCode int, helpExitCode int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	helpPath := filepath.Join(dir, "help.txt")
	if err := os.WriteFile(helpPath, []byte(helpText), 0o600); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(dir, "invocations")
	binPath := filepath.Join(dir, "fake-claude")
	script := `#!/bin/sh
printf '%s\n' "$*" >>"$CLAUDE_PREFLIGHT_RECORD"
case "$1" in
--version)
	printf '2.1.226 (Claude Code)\n'
	exit ` + strconv.Itoa(versionExitCode) + `
	;;
--help)
	cat "$CLAUDE_PREFLIGHT_HELP"
	exit ` + strconv.Itoa(helpExitCode) + `
	;;
*)
	printf 'unexpected claude invocation: %s\n' "$*" >&2
	exit ` + strconv.Itoa(preflightFakeUnexpectedArgsEc) + `
	;;
esac
`
	if err := os.WriteFile(binPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PREFLIGHT_RECORD", recordPath)
	t.Setenv("CLAUDE_PREFLIGHT_HELP", helpPath)
	return binPath, recordPath
}

func readCapturedClaudeHelp(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "claude-help-2.1.226.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readClaudeInvocations(t *testing.T, recordPath string) []string {
	t.Helper()
	data, err := os.ReadFile(recordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func missingFeatures(report claudePreflightReport) []string {
	features := []string{}
	for _, failure := range report.failures {
		if failure.category == preflightFeatureMissing {
			features = append(features, failure.feature)
		}
	}
	return features
}

func requireSingleFailureCategory(t *testing.T, report claudePreflightReport, category string) {
	t.Helper()
	if report.ok {
		t.Fatalf("%s境界でpreflightが通りました: %#v", category, report)
	}
	if len(report.failures) != 1 || report.failures[0].category != category {
		t.Fatalf("失敗分類 = %#v, want [%s]のみ", report.failures, category)
	}
}

// TestClaudePreflightPassesOnCapturedClaudeHelpはClaude Code 2.1.226のhelp snapshotで
// preflightがPASSすることと、起動が --version と --help の2回だけであることを固定する。
func TestClaudePreflightPassesOnCapturedClaudeHelp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	binPath, recordPath := writeFakeClaudePreflightBinary(t, readCapturedClaudeHelp(t), 0, 0)

	report := claudeCompatPreflight(binPath)

	if !report.ok {
		t.Fatalf("2.1.226 help snapshotでpreflightが落ちました: %#v", report.failures)
	}
	if report.version != "2.1.226 (Claude Code)" {
		t.Fatalf("version = %q", report.version)
	}
	invocations := readClaudeInvocations(t, recordPath)
	if len(invocations) != 2 || invocations[0] != "--version" || invocations[1] != "--help" {
		t.Fatalf("起動引数 = %#v, want [--version --help]の2回のみ", invocations)
	}
}

// TestClaudePreflightIdentifiesEachMissingFeatureは必須featureを1件ずつ欠落させた
// fixtureが、欠落flagを特定してfail closedになることを全inventory分固定する。
func TestClaudePreflightIdentifiesEachMissingFeature(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	help := readCapturedClaudeHelp(t)
	for _, feature := range claudePreflightFeatures {
		t.Run(feature.flag, func(t *testing.T) {
			binPath, _ := writeFakeClaudePreflightBinary(t, claudeHelpWithoutFlag(help, feature.flag), 0, 0)

			report := claudeCompatPreflight(binPath)

			if report.ok {
				t.Fatalf("%s欠落でpreflightが通りました", feature.flag)
			}
			missing := missingFeatures(report)
			if len(missing) != 1 || missing[0] != feature.flag {
				t.Fatalf("欠落特定 = %#v, want [%s]のみ", missing, feature.flag)
			}
		})
	}
}

// TestClaudePreflightDetectsOutputFormatVocabularyDriftはflag宣言が残っていても
// choices語彙から依存formatが消えた場合をfail closedで検出する境界である。
// --output-format blockの語彙だけを消し、--input-format等のblock外語彙は残す。
func TestClaudePreflightDetectsOutputFormatVocabularyDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	help := readCapturedClaudeHelp(t)
	block := claudeHelpOutputFormatBlock(help)
	outside := strings.Replace(help, block, "", 1)
	if !strings.Contains(outside, `"stream-json"`) {
		t.Fatal("fixtureがblock外のstream-json語彙を保持していません")
	}
	drifted := strings.Replace(help, block, strings.ReplaceAll(block, `"stream-json"`, `"streaming-json"`), 1)
	binPath, _ := writeFakeClaudePreflightBinary(t, drifted, 0, 0)

	report := claudeCompatPreflight(binPath)

	if report.ok {
		t.Fatal("stream-json語彙欠落でpreflightが通りました")
	}
	if missing := missingFeatures(report); len(missing) != 0 {
		t.Fatalf("flagは削っていません: %#v", missing)
	}
	if len(report.failures) != 1 || report.failures[0].category != preflightOutputFormatsMissing {
		t.Fatalf("失敗分類 = %#v, want [%s]のみ", report.failures, preflightOutputFormatsMissing)
	}
}

// TestClaudePreflightBinaryBoundariesはbinary不在(絶対path・PATH上なし)と
// 非実行可能の境界を、binary起動前にfail closedする分類で固定する。
func TestClaudePreflightBinaryBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	t.Run("absolute path absent", func(t *testing.T) {
		report := claudeCompatPreflight(filepath.Join(t.TempDir(), "missing-claude"))
		requireSingleFailureCategory(t, report, preflightBinaryAbsent)
	})
	t.Run("bare name not on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		report := claudeCompatPreflight("claude")
		requireSingleFailureCategory(t, report, preflightBinaryAbsent)
	})
	t.Run("not executable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "claude-noexec")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		report := claudeCompatPreflight(path)
		requireSingleFailureCategory(t, report, preflightBinaryNotExecutable)
	})
}

// TestClaudePreflightVersionAndHelpFailureBoundariesはversion/helpが非zero exitで
// 失敗する境界を固定する。version失敗後はhelpを起動せず即fail closedする。
func TestClaudePreflightVersionAndHelpFailureBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	help := readCapturedClaudeHelp(t)
	t.Run("version exits non-zero", func(t *testing.T) {
		binPath, recordPath := writeFakeClaudePreflightBinary(t, help, 1, 0)

		report := claudeCompatPreflight(binPath)

		requireSingleFailureCategory(t, report, preflightVersionFailed)
		if invocations := readClaudeInvocations(t, recordPath); len(invocations) != 1 || invocations[0] != "--version" {
			t.Fatalf("version失敗後もbinaryを起動しています: %#v", invocations)
		}
	})
	t.Run("help exits non-zero", func(t *testing.T) {
		binPath, _ := writeFakeClaudePreflightBinary(t, help, 0, 1)

		report := claudeCompatPreflight(binPath)

		requireSingleFailureCategory(t, report, preflightHelpFailed)
	})
}

// TestClaudePreflightInventoryMatchesRunnerSourceFlagsはrunner package本体がClaude CLI
// argvへ渡すflag string literalとclaudePreflightFeaturesのdriftを両方向で検出する。
// 走査はpackage内の非test .go file全体を対象とし、将来3箇所目の呼出fileが増えても
// flag追加を拾う。flagを足してinventoryを更新し忘れた場合と、inventoryに実装から
// 消えたflagが残った場合に失敗する。
func TestClaudePreflightInventoryMatchesRunnerSourceFlags(t *testing.T) {
	sourceFlags := map[string]bool{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("package dir読み込み: %v", err)
	}
	flagLiteral := regexp.MustCompile(`"(--?[A-Za-z][A-Za-z0-9_-]*)"`)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s読み込み: %v", name, err)
		}
		for _, literal := range flagLiteral.FindAllStringSubmatch(string(data), -1) {
			sourceFlags[literal[1]] = true
		}
	}
	inventoryFlags := map[string]bool{}
	for _, feature := range claudePreflightFeatures {
		if inventoryFlags[feature.flag] {
			t.Fatalf("inventory重複: %s", feature.flag)
		}
		inventoryFlags[feature.flag] = true
	}
	for flag := range sourceFlags {
		if !inventoryFlags[flag] {
			t.Errorf("実装が渡すflag %sがpreflight inventoryにありません", flag)
		}
	}
	for flag := range inventoryFlags {
		if !sourceFlags[flag] {
			t.Errorf("inventoryのflag %sがpackage本体にありません", flag)
		}
	}
}

// TestClaudePreflightLiveClaudeCLIは実Claude CLIに対してno-AI preflightが通ることを
// 確認する。binaryがない環境ではskipし、version番号は固定しない(form契約のみ検査)。
func TestClaudePreflightLiveClaudeCLI(t *testing.T) {
	bin := os.Getenv("GLM_WORKER_CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("claude CLIが利用できません: %v", err)
	}

	report := claudeCompatPreflight(bin)

	if !report.ok {
		t.Fatalf("実CLI preflight失敗: %#v", report.failures)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(report.version) {
		t.Fatalf("version出力形式 = %q", report.version)
	}
	t.Logf("live preflight PASS: %s", report.version)
}
