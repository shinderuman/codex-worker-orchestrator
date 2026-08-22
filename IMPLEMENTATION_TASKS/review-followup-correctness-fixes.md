# Task: 未レビューcommitで残ったPTY・task corpus・MODEL_IDLEの3不整合を修正する

## Original instruction

`````text
# Codexへの修正指示：未レビュー4コミットで残った3点を修正

前回レビュー済みHEAD `ba7396a` 以降を再レビューした結果、以下3点だけ修正する。
既存RULESの共通事項は再掲せず、新しい一般機構も増やさない。

## 1. PTY testからproductionに存在しないREADY同期を外して成立性を確認

`dd91ad3`の実PTY testは、子processがraw mode設定後に`ready`を書き、親testがそれを待ってからpayloadをfeedしている。

これはproductionより強い同期条件であり、
「caller側sttyもREADY handshakeも不要」
を検証できていない。

productionと同じく、

```text
process start → READY待ちなし → 即payload write
```

で実PTY testを行うこと。

UTF-8、改行、backtick、`$`、quote、NUL、Ctrl-C等を含め、複数回実行してstartup/feed raceの有無を確認する。

raceが実際に再現した場合だけ、Task 004契約どおり最小READY handshakeを設計判断へ戻す。
再現しないならproductionへhandshakeを追加しない。

## 2. task corpus conformance testをsection単位で判定

現在の`TestTaskCorpusScheduleStateConformance`はtask file全文を字面検索しているため、

* Original instruction内の`## Status`
* Original instruction等に書かれた`- \`IMPLEMENTATION_TASKS/...\``

までschedule metadata / dependencyとして誤認し得る。

losslessなOriginal instructionを壊さないよう、

* `Status`判定はtop-level `## Status` sectionだけ
* dependency検査はtop-level `## Dependencies` sectionだけ

を見ること。

一般Markdown parserは追加しない。
必要なsection境界だけを単純に扱う。

Original instruction内に同じ文字列を含むfixtureを追加し、誤検出しないことを固定する。

## 3. `MODEL_IDLE`を本当にmodel activity基準にする

現在の`MODEL_IDLE`は`LastEventAt`基準のため、長時間Bash中の`tool_progress`等でも更新される。

これでは「最後のmodel activityからの経過時間」にならない。

model activityとして扱うeventだけで別時刻を更新すること。
少なくともassistant側の、

* thinking
* text
* tool_use

をmodel activityとし、

* system `tool_progress`
* `task_notification`
* user `tool_result`
* background task状態通知

では`MODEL_IDLE`をリセットしない。

長時間Bash中に30秒ごとの`tool_progress`が流れても、`MODEL_IDLE`が増え続けるtestを追加する。

event JSONLへ本文を追加保存する必要はない。

## 完了確認

この3点以外を理由なく作り直さない。

特に、

* TARGETS修正
* `--decision` retry修正
* WORKER/REVIEWER requirement source統一
* schedule parser fail-closed
* PTY内部raw化
* `--watch --verbose`のlive snapshot方式

は今回のレビューでは維持対象。

修正後、なぜ既存worker/reviewer/Sol reviewで各問題を見逃したかもHistory用に短く報告すること。

優先順位はお前自身で判断しろ
`````

## Amendments

none

## Resolved references

- `dd91ad3`はTask 004 self-contained stdin PTYの最終commit系譜を指し、現HEADではamend後commit `dd91ad3`の内容が後続commitへ含まれている。
- `TestTaskCorpusScheduleStateConformance`は`glm-worker/internal/workflow/task_corpus_conformance_test.go`を指す。
- `MODEL_IDLE`は直前に完了したwatch verbose taskの表示項目を指す。

## Purpose

直近reviewで判明した3つのfalse-positive test/observability境界だけを修正し、成立済みcontractを維持する。

## Contract

- productionと同じREADY待ちなし即writeの実PTY testを複数回行いstartup/feed raceを検証する
- task corpus conformanceはtop-level Status/Dependencies sectionだけを単純抽出して判定し、lossless Original instruction本文を無視する
- MODEL_IDLEはassistant thinking/text/tool_useだけで更新するmodel activity時刻を使い、tool progress/result/background通知では更新しない

## Must not

- race再現なしにREADY handshakeをproductionへ追加しない
- 一般Markdown parserや新しい一般機構を追加しない
- 指定3点以外の成立済みTARGETS、decision retry、requirement source、schedule fail-closed、PTY raw化、live snapshot方式を作り直さない
- event JSONLへ本文を追加保存しない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- READY待ちなし即payload writeの実PTY testを複数回実行し、全特殊byteとstartup/feed raceを検証
- Original instruction内のStatus見出し・task path bulletを誤検出しないfixtureと、top-level sectionだけのpositive/negative test
- assistant thinking/text/tool_useと非model eventの分類test、30秒tool_progress中もMODEL_IDLE増加test
- 3点それぞれのescaped原因をHistory候補として報告
- 全test、race、vet、build、gofmt、独立review、必要なSol品質gate、親Codex commit、本配置

## Historical invariants

- Task 004 self-contained stdin PTY transport completion
- Task 001/002後のPlan単独schedule stateとtask corpus conformance
- watch verbose live snapshot・plain watch/event JSONL不変contract

## Dependencies

none

## Review findings

- productionにないREADY同期、全文字面検索、model/non-model activity混同の3件

## Current boundary

instruction fixed-context audit完了後の割り込み最優先taskとしてtracked化。未着手。
