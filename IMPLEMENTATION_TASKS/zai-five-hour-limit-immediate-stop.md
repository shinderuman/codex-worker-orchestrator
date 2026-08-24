# Task: Z.ai 5時間Usage Limit検出時に即時RATE_LIMITEDへ停止する

## Original instruction

````text
優先順位はお前が決めろ



# Codex指示：5時間Usage Limit検出時の無意味な再試行を廃止する

Z.ai / GLM の **5時間Usage Limitを検出した場合、再試行せず即座に `RATE_LIMITED` 待機へ移行する**よう修正する。

通常の一時的provider障害のretry/recoveryとは混同しないこと。

## 現在の問題

`glm-worker` は5時間上限を通常の429等とは別分類しているが、Claude CLI側が内部retryした後でなければ `glm-worker` がLIMITを処理できない経路があり、quota枯渇後も数分無意味に待たされることがある。

5時間上限はprovider自身が、

- usage limit reached
- 5 hour limit
- reset時刻

を返しており、reset前に再試行して成功する性質ではない。

したがってLIMIT確定後のretryは不要。

## 要件

Z.aiの5時間Usage Limitを確実に識別できた最初の時点で、

1. Claude subprocessの追加retryを止める
2. 現在の処理を中断する
3. 既存のcheckpoint/sessionを保持する
4. 既存の `RATE_LIMITED` stateへ遷移する
5. providerが返したreset時刻を使って既存のauto-resume待機へ入る

**LIMIT検出後の再確認retryは0回とする。**

1回だけ確認する処理も不要。

## 重要な境界

変更対象は **5時間Usage Limitだけ**。

以下の既存挙動を変えないこと。

- 通常の一時429
- overload
- 502 / 503 / 504 / 529等
- network error
- transient provider recovery
- probe/backoff/resume
- checkpoint保持
- session resume

`CLAUDE_CODE_MAX_RETRIES=0` のようにClaude CLI全体のretryを無効化してはいけない。

それでは一時障害まで即失敗するため、既存のtransient recovery contractを壊す。

## 実装

現在どこで最初の5h LIMIT情報を観測できるかをrunnerで確認すること。

stderrまたはstream eventとしてClaude CLI終了前にLIMITを認識できるなら、その時点でexactな5h LIMITを分類し、Claude subprocessを終了させる。

判定は既存の5h LIMIT分類を単一の正として再利用し、runnerとworkflowで別々の文字列判定を増やさない。

process終了後にも同じLIMITを再処理して二重遷移・二重timer設定しないようにする。

新しいgeneric retry frameworkや複雑なstate machineは作らない。

## 判定精度

単なるHTTP 429だけでは5h LIMIT扱いしない。

既存契約どおり、Z.aiの5時間上限であることを十分に特定できる情報、少なくとも現在利用している `[1308]` / 5-hour usage-limit signalを基準にする。

曖昧な429は従来のtransient経路へ残す。

## テスト

少なくとも以下を追加・更新する。

1. 最初の5h LIMIT signalを受けた時点でchild処理が終了する
2. LIMIT後に追加retryを行わない
3. `RATE_LIMITED` が一度だけ保存される
4. checkpoint/sessionが保持される
5. reset時刻を使った既存auto-resume設定が維持される
6. 通常の429は5h LIMIT扱いされない
7. transient provider failureの既存retry/probe/backoff testがそのまま通る
8. 5h LIMIT判定がrunner/workflowで重複実装されていないことを確認する
9. `go test`、race、vet、build、gofmtを通す

実processを使った既存runner test infrastructureで「LIMITを出した後も動き続けるfake child」を表現できるなら、LIMIT検出後に即terminateされ追加出力まで進まないことを直接検証する。

## scope

目的は、

**5時間quotaが尽きたことが分かった後に数分待つ無駄をなくし、即座に待機状態へ移行すること**

だけ。

他のretry policyの再設計へ広げない。

既存ACTIVE taskに別の未コミット変更がある場合は混ぜない。

GLMにcommit/pushさせない。\
pushしない。
````

## Amendments

### 2026-08-24 follow-up 1

````text
429で5hのLIMIT時はリトライしないように修正し終えたんだよな？
いまの5hLIMIT時にリトライしていたような気がするのでログをGLMじゃなくお前が直接確認してみてくれ
````

### 2026-08-24 follow-up 2

````text
いまのうちに claude -p 'hello' とか直接叩いてみてなにが起きてるのかデバッグしてみればいいんじゃないの
GLM自身はそれを観測することができないんだし
````

### 2026-08-24 follow-up 3

````text
429が出たら即中断してmax_retriesを1にして再リクエストすればいいということか？
しかしそこまで複雑なロジックがこの対応に必要か？
諦めて最長リトライをし続けるのとどちらが良いか？

一番重要なことは
Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

であることを忘れないでどうすればいいか考えてくれ
````

### 2026-08-24 follow-up 4

````text
じゃあ撤去する形でタスクを積んでくれ
優先順位の判断はお前に任せる
````

### 2026-08-24 follow-up 5

````text
ちょっと待て、先頭のコミットがこれの対応ならRevert Commitすればいいだけの話なんじゃないの？
再開整合性を崩してまでというのはわかるが新規作業として取り掛からせるにはそれもコストかけすぎじゃないのか？
本当に新規タスクで積んだほうがいいのか？
````

### 2026-08-24 follow-up 6

````text
じゃあそれで進めろ
あとこの件で一番気になったのはお前がゲートとして機能していなかったことと、そもそも事前にPoC等をやっていなかったことだ
そしてGLMも言われたことをやるだけやって結局何も確認できていなかったことだ
これらは改善すべきではないか
````

## Resolved references

- 「現在の問題」はZ.ai Coding Planの5時間quota枯渇後、exact limit signalがClaude CLIのstream/stderrへ現れてもchild内部retry終了までwrapperの既存`RATE_LIMITED`処理へ移れない経路を指す。
- Original instruction作成時の「現在ACTIVEのTask 007」はtask ID `820e121c-4a18-4c41-b644-86d18a850896`を指す。現在は完了済みであり、reopen時のACTIVEはTask 008である。
- 「撤去」「Revert Commit」は、commit `cbf71c7`全体でPlan/Historyを巻き戻すことではなく、early-stopのproduction code/testだけを親Codexが選択的に逆適用することを指す。
- 「それで進めろ」は、新規GLM task/sessionを起動せず、Task 008完了後に親Codexが機械的逆適用と既知testで処理する直前提案への承認を指す。

## Purpose

実Claude CLIではexact 5h signalが全retry終了後まで外部化されないというPoC結果を正とし、効果のないearly-stop実装を選択的にrevertして既存のterminal分類へ戻す。

## Contract

- 最新Amendmentは当初の即時停止実装要求をoverrideする。defaultのClaude CLI retryをboundedな既知制約として受容する
- commit `cbf71c7`のproduction code/test部分だけを機械的に逆適用し、Task 008以降のPlan・History・task lifecycle状態は巻き戻さない
- exact 5h signalはretry終了後のterminal assistant/resultから既存classifierで分類し、checkpoint、session、reset時刻、RATE_LIMITED、auto-resume semanticsを維持する
- `CLAUDE_CODE_MAX_RETRIES=0`、最初のgeneric 429でkillして別processを再要求する二段階処理、retry ownership移管は採用しない
- 実producer PoC不足、人工fixtureとproduction event shapeの不一致、親Sol gateの受領失敗はTask 015のfeasibility gate改善へ引き継ぐ
- Task 008のrate-limit resume・session・checkpoint・未コミットdiffを完了前に変更せず、Task 008の安全な完了境界後に親Codexが選択的revertする

## Must not

- commit `cbf71c7`全体をraw revertしてPlan、History、Task 008 ACTIVE状態を過去へ戻さない
- 新しいGLM実装session、動的kill/probe state machine、provider retry policy再設計を追加しない
- generic 429、overload、5xx、network errorの既存transient recoveryを変更しない
- Task 008のworking tree、task state、session、checkpointを破棄・resetしない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- `limit_stop.go`とearly-stop専用testを削除し、`runner.go` / `stream_events.go`の該当production差分が`cbf71c7^`と機械的に一致する
- default retry終了後のexact 5h terminal結果からRATE_LIMITEDへ遷移し、checkpoint/session/reset時刻/auto-resumeを維持する既存testが通る
- terminal assistant event観測時にchildをkillして直後のresult eventを失う経路が残らない
- Historyの「追加retry 0回で完了」というfalse-complete記録を、実PoC・No-Go・選択的revert結果へ訂正する
- Task 008完了後、親Codexが選択的revert、関連test・全test/race/vet/build/gofmt、source/installed一致、installed smokeを確認してcommitする
- exact inverseと既知testで確認できるため、新しいGLM実装sessionや設計reviewを起動しない

## Historical invariants

- Z.ai `[1308]` / 5-hour usage-limit signalは通常429と区別する
- RATE_LIMITEDは正常な一時停止でありWORKER_ERRORへ変換しない
- working tree、task state、session、checkpointを破棄しない

## Dependencies

none

## Review findings

- Task 008 live runは1 model call・resume/probe 0回だったが、Claude CLI内部で`system/api_retry`が10回発生した
- 親Codexが直接実行した`claude -p 'hello' --output-format stream-json --verbose`ではattempt 1-10のeventが`error_status:429` / `error:"rate_limit"`だけを持ち、`[1308]`・5h・reset時刻は全retry終了後のsynthetic assistant/resultで初めて出た
- `CLAUDE_CODE_MAX_RETRIES=0`の直接PoCでは約0.6秒でexact terminal結果を得たが、usageは0であり、動的制御による節約は主に約3分のwall-clockだけ。最上位Evalに対する利益よりtransient回復境界の複雑化riskが大きい
- `cbf71c7`の人工testはproductionにないexact本文入り途中eventを与え、実producerの`api_retry` schema/timingを確認していなかった
- 現early-stopは全retry後のsynthetic assistantを見てkillするため時間を短縮せず、直後のterminal resultだけを失わせる

## Current boundary

ACTIVE Task 008はtask ID `6d6dbb53-acc7-4567-8af1-acc82b4ac0c5`でrate-limited停止中。parent-managed metadata更新として本taskをreopenしNEXT最上位へ置くが、Task 008のsession/checkpoint/working treeを維持して同task完了後に選択的revertする。
