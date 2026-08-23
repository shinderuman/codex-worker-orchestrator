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

none

## Resolved references

- 「現在の問題」はZ.ai Coding Planの5時間quota枯渇後、exact limit signalがClaude CLIのstream/stderrへ現れてもchild内部retry終了までwrapperの既存`RATE_LIMITED`処理へ移れない経路を指す。
- 現在ACTIVEのTask 007はtask ID `820e121c-4a18-4c41-b644-86d18a850896`でrate-limited停止中であり、同taskの未コミットJSON/JSONL統一diff、session、checkpoint、automationへ本taskを混ぜない。

## Purpose

Z.ai 5時間Usage Limitを確定できた最初のstream/stderr観測でClaude subprocessを止め、追加retryなしに既存RATE_LIMITED/checkpoint/auto-resume経路へ移す。

## Contract

- exact 5h limit classifierを単一の正としてrunnerの早期観測とworkflow終端で共有する
- exact signal観測後はchildを即時終了し、LIMIT再確認retryを0回にする
- checkpoint、worker/reviewer session、reset時刻、既存auto-resume semanticsを維持する
- process終了後の同一limit再処理でstate遷移・timer・telemetryを二重化しない
- 曖昧なHTTP 429と通常transient failureは既存retry/probe/backoff/resume経路へ残す

## Must not

- Claude CLI全体のretryを環境変数等で無効化しない
- 5h limit以外のprovider recovery policyを変更しない
- runner/workflowへ別々の文字列matcherを実装しない
- generic retry frameworkまたは複雑なstate machineへ拡張しない
- ACTIVE Task 007の未コミットdiffへ混ぜない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- fake childがexact 5h limitを出した後も動き続ける場合、最初のsignalでterminateされ後続出力・retryへ進まない
- exact limit後のretry 0回とRATE_LIMITED保存 exactly onceをproduction pathで固定
- checkpoint/session保持とprovider reset時刻によるauto-resumeを固定
- generic 429、overload、5xx、network errorの既存transient testsが不変
- 5h classifierのrunner/workflow重複がない
- gofmt、go vet、全test、race、build、独立review、必要なSol gate、親Codex commit

## Historical invariants

- Z.ai `[1308]` / 5-hour usage-limit signalは通常429と区別する
- RATE_LIMITEDは正常な一時停止でありWORKER_ERRORへ変換しない
- working tree、task state、session、checkpointを破棄しない

## Dependencies

none

## Review findings

none

## Current boundary

Task 007とcompletion detection incidentの後、Task 008より前のNEXT。ACTIVE Task 007のrate-limit resumeと未コミットdiffを変更せず、独立taskとして開始する。
