# full install smoke証拠の取得と再利用

`glm-worker --install-smoke`は、full install smoke(`tests/install_smoke.sh`)の取得・確認の単一入口である。
worker・reviewer・fix再取得・親final gateの全境界がこの入口を使い、`tests/install_smoke.sh`を直接実行して証拠を取得しない。
未配置環境では`glm-worker` module rootから`go run ./cmd/glm-worker`同等の引数で実行してよい。

## 実行と再利用の契約

- 取得round・gateは`--role worker|reviewer|fix|parent`で申告する。申告は証拠ledgerの観測用であり、再利用判定には影響しない。
- この入口のmachine出力は成功時のstdout JSON object 1件だけである。失敗時はstdoutへ出力せず、stderrのstructured process error JSON(`kind:"install_smoke_failed"`)と非zero exitで伝える。内部smoke logをmachine stdout/stderrへ混在させず、結果JSONの`log`に保存先file pathだけを載せる。
- この入口は実行前に現在のidentityを機械取得し、一致する過去PASSがあればsmokeを実行せず`status:"reused"`として当該証拠(identity・result・完了時点・取得role)を返す。
- reusedを受け取った境界はfull smokeを再実行せず、その証拠を品質確認の根拠として引用する。自然言語の「以前PASSした」やtimestampだけでは再実行を省略しない。
- 一致するPASSがなく、stale・異環境・失敗証拠・証拠欠損のいずれかであれば、smokeを実行して結果を証拠ledgerへ記録する。失敗時は非zero exitで失敗を伝える。
- sourceやsmoke-relevant input・環境が変わった場合の旧証拠失効はidentity照合が自動的に強制する。変更後snapshotへ古いPASSを手動で適用しない。

## identity構成

再利用判定は次の全軸一致だけを認める。ledger破損・identity取得失敗は再利用せず失敗する。

- source snapshot: 対象repoのworking tree内容digest(tracked・untracked両方、parent管理metadata(`IMPLEMENTATION_RULES.md`・`IMPLEMENTATION_PLAN.local.md`・`IMPLEMENTATION_HISTORY.md`・`IMPLEMENTATION_TASKS/`)は除外)。commit前後で内容が同一なら同一identityであり、親final gateはcommit後もworker/reviewerのPASSを再利用できる。
- smoke-relevant input: `install.sh`・`tests/install_smoke.sh`・`codex/`・`claude/`・`glm-worker/`・`tools/`・`.githooks/`の内容digest。parent管理metadata-only変更はこのdigestを変えない。
- toolchain/environment: `go version`・GOOS・GOARCH・platform(uname)・claude CLI契約probe(`--json-schema`対応)。
- result・完了時点・exit code・durationは証拠record本体に保存する。

## round/gate責務

- workerは最終candidateについてこの入口からfull smoke証拠を取得する。修正途中の確認は必要なtargeted verificationを使い、毎round full smokeを取り直さない。
- reviewerは再確認にこの入口を使い、同一snapshotならreusedをworker証拠の共有として扱う。
- fixでsmoke-relevant inputが変わった場合、workerは再取得だけを行い、旧証拠の引用をやめる。
- 親final gate・`install.sh`本配置後のproduction smoke確認も同じ入口で行い、同一identityなら再実行しない。
