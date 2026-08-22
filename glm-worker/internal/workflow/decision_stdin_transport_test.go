package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecisionStdinTransportContractWiringはdecision/fix stdin modeの親caller contractを
// glm-execution.md本文の必須契約文で固定する。transportは自己完結で、TTY/PTYのterminal mode
// 設定・復元はglm-worker内部責務のため、caller側`stty` recipe・echo無効化・terminal設定の
// 各契約文が本文へ再混入した場合や、byte数・SHA以外の本文由来情報をcommandへ載せる契約が
// 欠けた場合は失敗する。GLM_STDIN_READY marker確認後の1回write・未観測/重複/先行終了の
// fail closed・markerをtransport controlとしてterminal payloadへ含めない各契約も固定する。
// 旧caller-side recipe固定形と、CLI内部機構(raw/noecho・termios)の詳細本文の再混入も拒否する。
func TestDecisionStdinTransportContractWiring(t *testing.T) {
	root := scenarioRepoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "codex", "instructions", "glm-execution.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)

	wires := []string{
		"exec_commandは`tty: true`で起動する",
		"`tty: false`へのfallbackを禁止する",
		"glm-worker --decision-stdin <payload-bytes>",
		"terminal mode設定はglm-workerのinvocation内責務",
		"callerは`stty`・raw mode・echo等のterminal設定を一切行わない",
		"command文字列へ入れてよい本文由来の情報はUTF-8 byte長と任意のSHA-256だけに限る",
		"固定marker行`GLM_STDIN_READY`の確認だけである",
		"TTY stdinのterminal設定適用に成功した直後に1回だけ出し",
		"pipe/file等の非TTY stdinでは出ない",
		"marker未観測・marker行の重複・processの先行終了では本文を未送信のままfail closed",
		"marker待ちの間に本文を先行writeしない",
		"marker確認後、呼び出しがsession化してsession IDを返した場合は",
		"末尾改行の有無に依存せず非emptyの`write_stdin`で本文全体を1回だけ送る",
		"改行だけの追加writeを行わない",
		"本文の分割再送・短文化・`--decision`/`--fix`へのargv埋込みfallbackを行わない",
		"transport controlであり、受理対象のterminal payload・machine result・単一描画の本文へ含めない",
		"この固定wrapper command自体はCodex tool側でsandbox外実行する",
		"glm-workerが既存task state/checkpoint/sessionを更新するためである",
		"毎回の再承認要求を本契約へ含めない",
	}
	for _, wire := range wires {
		if !strings.Contains(doc, wire) {
			t.Errorf("glm-execution.md lacks stdin transport wiring: %q", wire)
		}
	}
	// 旧caller-side stty recipe・弱い固定形が契約本文へ残留しないことを固定する。
	// sandbox外実行の理由はstate更新権限であり、terminal制御の成立場所ではない。
	// raw/noecho・termios等の内部機構詳細はCLI契約(README・PTY test)が持ち、
	// 毎回読むcaller instructionへは載せない。
	for _, weak := range []string{
		"stty raw -echo",
		"`stty`を適用",
		"stty設定が失敗した場合はglm-workerを起動せず",
		"-icanon min 1 time 0",
		"stty -echo -icanon",
		"sandbox内へ落ちたshell wrapperでは`stty`による端末制御が成立しない",
		"raw/noecho",
		"termios",
	} {
		if strings.Contains(doc, weak) {
			t.Errorf("glm-execution.md still contains outdated transport contract: %q", weak)
		}
	}
}
