# Task: `--watch --verbose`で実行中toolのlive observabilityを追加する

## Original instruction

`````text
# Codexへの指示：`--watch`に実行中toolの詳細表示を追加

現在の`glm-worker --watch`は生存確認には使えるが、`tool_progress`だけでは長時間処理が何をしているか判断できない。

Codexを途中介入させずGLMを長時間自走させるため、人間が「正常な長時間処理か」を判断できる観測性を追加する。

## 要件

既存の`--watch`表示は維持し、詳細表示を明示指定できるようにする。

第一候補:
```bash
glm-worker --watch --verbose
```

verbose時は少なくとも以下を表示する。

- task全体の経過時間
- 最後のmodel activityからの経過時間
- 現在実行中のtool名
- 現在toolの経過時間
- Bashなら実行command
- tool側にdescription/purposeがあれば表示
- background task待機中なら、そのtaskと待機状態
- 直前に完了した長時間toolの種類・所要時間
- 直近のtool error

例:
```text
TASK AGE   02:50:46
MODEL IDLE 04:31

CURRENT    Bash  04:31 elapsed
COMMAND    sleep 295; grep -c 'GATE-START' /tmp/instr4_full.log ...
PURPOSE    Check fourth run at gate section

LAST       Bash completed 295.1s
```

## 実装境界

Claude Codeの`~/.claude/projects/...jsonl`を解析する方式にはしない。

glm-workerが既に受信しているstream eventから現在toolの状態を組み立てる。

現在のevent JSONLへcommand・thinking本文・tool入出力本文を新たに永続保存しない。
今回の目的はlive observabilityであり、event logを詳細transcript化しない。

command等が長い場合はwatch表示上だけ適切にtruncateし、通常のevent logサイズを増やさない。

`--watch`単体の既存表示・retention・event schemaを不要に変更しない。

## Acceptance

- `--watch`の既存動作が変わらない
- `--watch --verbose`で実行中Bashのcommandとelapsedを確認できる
- 長時間toolの実行中にelapsedが更新される
- tool完了後はCURRENTから外れ、直前の長時間toolとして確認できる
- background task待機を「停止」と誤認しない表示になる
- tool errorをverbose表示から確認できる
- command/tool本文がevent JSONLへ新規保存されない
- Claude Code内部session JSONLのpath/schemaへ依存しない

今回の目的は「デバッグ機能を増やす」より、**GLMを数時間放置してもCodexを呼ばず、人間がwatchだけで正常性を判断できるようにする**ことなので、その境界を明示しています。
`````

## Amendments

- none

## Resolved references

- `--watch`は、authoritative task statusがactiveを離れた時点で最終eventをdrainして終了する現行`glm-worker --watch`を指す。
- 「glm-workerが既に受信しているstream event」はprovider runnerがliveに受信するstream eventを指し、Claude Code内部session JSONLは参照対象外とする。

## Purpose

長時間自走中のGLMをCodexの途中介入なしで放置できるよう、人間がwatchだけで現在tool・経過・待機・直近errorを判断できるlive observabilityを追加する。

## Contract

- 既存`--watch`を不変に保ち、明示的なverbose optionでのみ詳細表示を有効にする
- glm-workerがlive受信するstream eventから、task age、model idle、current tool、tool elapsed、Bash command、purpose、background wait、直前の長時間tool、直近tool errorを組み立てる
- elapsedはtool実行中に更新し、完了toolはCURRENTからLASTへ遷移させる
- command等の詳細はwatch表示時だけtruncateし、通常event JSONLへ本文を永続化しない

## Must not

- Claude Codeの`~/.claude/projects/...jsonl` path/schemaへ依存しない
- event JSONLをcommand・thinking・tool input/output本文を持つ詳細transcriptへ変えない
- `--watch`単体の表示、retention、event schemaを不要に変更しない
- Codexによる途中介入を正常性確認の前提にしない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- `--watch`既存動作の回帰test
- `--watch --verbose`で実行中Bashのcommand・tool elapsedが表示され、elapsedが更新される
- tool完了後にCURRENTから外れ、直前の長時間tool種類・所要時間がLASTに表示される
- background taskと待機状態、purpose/description、直近tool errorをverbose表示で確認できる
- 長いcommandは表示時だけtruncateされる
- event JSONLにcommand・thinking・tool入出力本文が新規保存されないことをtestで固定
- Claude Code内部session JSONL非依存を実装・testで確認
- 全test、race、vet、build、gofmt、独立review、risk/contractに応じたSol品質gate、親Codex commit、本配置を完了

## Historical invariants

- `IMPLEMENTATION_HISTORY.md`の`--watch`終端後hang対策
- 既存`--watch`のauthoritative status終端・最終event drain contract
- machine-only event logのretention・schema・payload非永続化方針

## Dependencies

none

## Review findings

- none

## Current boundary

新規割り込み要求をtracked化。Task 004完了後のNEXT先頭で未着手。
