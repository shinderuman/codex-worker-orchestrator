# 総合レビューの再現証拠

Finding本文・影響・限界・修正方向はルートの `Review.md` を正とする。このdirectoryは一時directoryが消えてもレビューの根拠を保持するための記録であり、productionへの修正ではない。

- `*.go.txt`: production関数と既存fixtureを呼ぶ再現test。期待する安全動作をassertするため、レビュー時点では不具合によりFAILする。
- `reproductions.json`: testとpackageの対応、および取得済み失敗診断。完全なログではなく、元ログ名と行番号を保持した抜粋。F2/F4の過去ログはこの抜粋に含まず、再現コードとReview本文を保持した。

## 手動再現手順

実行helper fileは持たず、選んだfixtureだけを標準のGo overlayで手動実行する。overlayは対象packageに存在しない `review_audit_test.go` をfixtureへ割り当てるため、production fileも既存test fileも変更しない。overlay JSONと結果logは一時directoryへ置き、repositoryへfileを追加しない。

1. `reproductions.json` の `fixtures` から再現する `file` と対応する `package` を選ぶ。以下は `taskdiff_audit_test.go.txt` と `taskdiff` の例で、他のfixtureでは手順3と4のpackage名とfile名を読み替える。
2. 一時directoryを作る。

   ```sh
   root=$(pwd)
   scratch=$(mktemp -d)
   ```

3. `$scratch/overlay.json` を手動で書く。`Replace` のkeyは対象packageへ新規追加する `review_audit_test.go` の絶対path、valueはfixture fileの絶対pathで、`<repo>` はこのrepositoryのcheckout root絶対pathに置き換える。pathはJSON stringへ書くため空白はそのままでよいが、path内の `"` と `\` はJSON escaping(`\"`、`\\`)が要る。

   ```json
   {"Replace": {"<repo>/glm-worker/internal/taskdiff/review_audit_test.go": "<repo>/review-evidence/taskdiff_audit_test.go.txt"}}
   ```

4. Go版を `quality-tools.yml` の `go:` 値から取り、`GOPROXY=off` と `GOCACHE` を設定して、`glm-worker` moduleの中から対象packageだけを実行する。stdoutとstderrは両方JSONL logへ保存し、終了codeは直後に変数へ保存する。

   ```sh
   version=$(sed -n 's/^go: //p' "$root/quality-tools.yml")
   (cd "$root/glm-worker" && env "GOTOOLCHAIN=go$version" GOPROXY=off \
       GOCACHE="${TMPDIR:-/tmp}/codex-worker-orchestrator-go-$version" \
       go test -json -count=1 -overlay "$scratch/overlay.json" \
       -run '^TestAudit' ./internal/taskdiff > "$scratch/result.jsonl" 2>&1)
   code=$?
   ```

5. 全体成否は `$code`、test別成否は `$scratch/result.jsonl` の `Action` が `pass`/`fail`/`skip` の行、diagnosticは `review_audit_test.go:` を含む `Output` 行で確認する。`$code` が0以外でtestの `Action` 行がない場合は再現FAILではなく手順側の失敗(overlay JSONの構文error、overlay fileの読込失敗、compile error等)なので、JSONLのerror出力を先に確認する。

fixtureのGit操作は一時repository内に限定され、GLM・外部model・本番設定を使わない。ShellCheckの再現fixtureは当時の正規配置先にある0.11.0を利用する。

fixtureはレビュー当時のsource構造に対する記録であり、その後のrefactorへ自動追従するtest suiteではない。特にevidence/targetのfixtureは旧app ownerを呼ぶが、計画化時点ではparentevidenceへ移動済み。実装開始時は各Taskの正規ownerへ再現を移し、古いapp APIの互換層を復活させない。保存済み診断はレビュー当時の実行結果であり、最新HEADの再実行結果とは区別する。

後続モデルへの引継ぎ範囲は保存済み結果のコミットのみ。レビューの追加・実装は依頼されていない。
