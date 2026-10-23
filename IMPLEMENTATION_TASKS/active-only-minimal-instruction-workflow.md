# Task: 短いTASK指示のみでACTIVE完了・NEXT未開始を成立させる

## Original instruction

````text
```
TASK

* 現在のACTIVE taskだけを完了させる。
* ACTIVE task完了後はNEXTを開始せず停止する。
```

この指示だけで作業が進められるか整備する、というタスクを作ってバグ修正系の後ぐらいに入れろ

さっきのテストの見直しよりは前でいいと思う

ついでにいまのタスクリストの順序も見直せ

見直しした結果はPRにはすぐ反映せず俺に教えろ
````

### 2026-10-24 追加原要求（旧RuntimeによるPublication阻害）

````text
じゃあこのタスクにそれを含めるべきなんじゃないのか
AmendmentじゃなくてOriginal Instructionとして
````

## Amendments

none

## Resolved references

- 2026-10-24追加原要求の「それ」: [Audit #1377 R03](https://github.com/shinderuman/codex-worker-orchestrator/issues/1377)で観測された、2026-10-23の現ACTIVE候補がpromote後のPublicationで停止した事象。10月7日から残存する**別Taskの未完了NEXT/successor Runtime**がparent未完了扱いでPublicationを阻害し、ユーザーが失敗扱いの保全・終了を個別承認した後、Codexが一時Python operatorで38ファイルを保全・archiveして再開した。これは正規Lifecycleの終了・復旧能力の不足として本Taskに含めるもので、単にその旧Runtimeを削除して通すことや同じ一時処理を常設する要求ではない。
- 「この指示」: 冒頭の `TASK` ブロックだけ。新規Codexセッションで追加のTask名・Issue番号・bootstrap/再開手順をユーザーが指定しなくても、現在のACTIVEを正規に完了させる状態へ整備することが本Taskの目的。現在の実行可能性を調査して終えるTaskではない。
- 「現在のACTIVE」: 指示を受け取った時点で、canonical Planに登録されているACTIVE Task。作成時の特定Taskへ固定しない。
- 「バグ修正系の後」「テストの見直しより前」: 本TaskはWorktree/CLI修正の後、自己確認型テスト見直し3件より前に配置する。
- Task順序の見直しはTask作成時の会話で実施済み。ユーザーはPR #1342で追加したTaskの順序を変更しない方針を指定した。未承認の並べ替えは本PRへ反映せず、この実装Taskの成果にも含めない。
- Rulesの通常Task完了に必要なPlan/Task metadata同期と、NEXT Taskの**実行開始**は別の境界として扱う。

## Purpose

ユーザーが上記の短い `TASK` ブロックだけを指示すれば、Codexが現行repository authorityからACTIVEを特定し、通常の調査・実装・独立Review・Validation・install・Publication・Retireまで完了して、NEXTを開始せず停止できるWorkflowを成立させる。

## External feasibility

status: not-applicable

## Contract

- 実行時current Git、`AGENTS.md`、`codex/AGENTS.md`、Rules、Plan、ACTIVE TaskとController/Runtimeのcanonical authorityから、入口・handoff・completion・停止までの欠落を特定して必要な箇所を修正する。会話の記憶や省略された追加手順をauthorityにしない。
- 正規のTask/Attempt/bootstrap/lease、GLM worker・reviewer、Sol判断、品質ゲート、install、Git/remote Publication、Retire、Plan/Task metadata同期を既存ownerとGuardを維持して実行できるようにする。
- ACTIVEのPublication時に、過去の別Taskに属する未完了・失敗停止済みNEXT/successor Runtimeが残存しても、現ACTIVEを永久に閉塞させず、既存のController/Runtime authorityに基づく**正規の機械的disposition・保全・再入**で処理できるようにする。終了に明示承認が必要なら合法な次操作として提示して承認を得るまでfail-closedで停止し、旧Taskを成功扱いせず履歴・証拠を保持する。個別Runtime ID依存の一時operatorや手作業のState移動を正規経路の前提にしない。
- ACTIVE Taskの正常完了に必要なNEXTへのmetadata上の昇格は既存Rulesに従う一方、**その昇格先のTaskを新規bootstrap・dispatch・調査・実装・resumeしない**。自動継続・automationも同じ停止境界を守る。
- 不整合、permission不足、provider制限、品質不合格は既存の正規回復/STOPで扱い、未完了を成功扱いしない。必要なproduction修正とE2E検証を実施する。新しい第二state正本・daemon・schedulerや専用復旧バイパスを増やさない。

## Must not

- 実行可能性の調査・手順書作成だけで完了扱いにしない。
- 次のNEXT Taskを開始しない。次Taskへ移行するためのModel呼出しや不正な実装先読みを行わない。
- Guard、source-of-truth、1 Task 1 Commit、独立Review、fail-closed、Publication/Retireの契約を弱めない。
- ユーザーに長文のbootstrap指示・Task名・Issue番号・コマンドの追加入力を要求する仕組みを前提にしない。
- 古い別TaskのRuntimeを無条件に削除・Complete化したり、未完了parent actionやPublication Guardを無視して現ACTIVEを通したりしない。例外的な手動archive/State書換えを恒久的な運用手順としない。
- 既存Planの優先順を無断で変更しない。旧schema互換や第二の状態管理を導入しない。

## Acceptance criteria

- 別の新規セッションで上記 `TASK` ブロックのみを入力し、正規Plan/ACTIVEを特定して通常のTask実行を開始できる。
- ACTIVEの必要なReview・Validation・install・Publication・RetireとMetadata同期を通したEnd-to-Endの完了証拠を確認できる。
- 完了後はNEXTへの実行側のbootstrap、dispatch、Model呼出し、mutationが発生せずSTOPする。metadataの正規昇格とは区別する。
- 失敗/復旧・長時間中断・再入・stale stateの代表ケースでもSTOP境界とfail-closedを保持する。
- #1377 R03相当の**他Taskの未完了NEXT/successor Runtimeが残る状態**をfixtureで再現し、Publicationの正常な停止・旧Runtimeの証拠を失わない明示的disposition（必要なら承認待ち）・同じACTIVEの正規Publication/Retireへの復帰をE2Eで検証する。新規Taskの自動開始、一時Python operator、手動State surgery、旧Taskの偽の成功判定、余計なRemote commitを必要としない。
- 必要な修正が通常のtest、Lint、関係するinstall/smokeで検証され、追加の独自制御機構を必要としない。

## Historical invariants

- 現行Git / Rules / Plan / ACTIVEとcanonical Controller/Runtimeを正とする。
- 一つのTaskの完了と次Taskの実行開始を厳密に分離する。
- Codex ReductionとQuality Deltaを最上位目的とし、既存の安全・証拠・Review境界を維持する。

## Dependencies

none
