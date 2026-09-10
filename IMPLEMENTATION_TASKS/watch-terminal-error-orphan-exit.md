# Task: Watch terminal-error orphan exit

## Original instruction

````text
glm処理中というわけでもなく永久に返ってこないコマンドの結果を待っているように見えたので止めた
作業再開しろ
````

## Amendments

### 2026-09-10

````text
現在行っている `refs/codex/*` の過去状態復元・digest逆算・SQLite/rollout履歴探索を直ちに打ち切ってください。

これは説明要求ではありません。開発作業へ戻るためのrun-control指示です。

既に次の事実は十分に確定しています。

- guardはGit ref集合を固定比較している
- Codex Desktop自身が `refs/codex/turn-diffs/*` を継続的に追加・削除・更新する
- `refs/codex/snapshots/*` もDesktop管理の可変refである
- そのため保存済みfull-ref digestを過去状態へ復元しようとしても、Desktopが並行して状態を変更するため収束しない
- HEAD / index / worktreeの変更とは別問題である
- 同じ復元を何度繰り返しても開発作業へ戻れない

したがって、これ以上次を行ってはなりません。
```text
- 過去のfull-ref digestに一致する集合の探索
- 削除済みDesktop ref名のSQLite/rollout履歴からの復元
- refs/codex/turn-diffs/* の手動復元
- refs/codex/snapshots/* の手動復元
- digest一致候補の総当たり
- 同じguard recoveryの反復
- resume-state.jsonを現在digestへ合わせるだけの応急処置
- この処理を行うためのユーザーへの追加許可要求
```

## 今すぐ行う修正

guard実装を修正し、Codex Desktopが所有するvolatile ref namespaceである次の2つをGit authority ref digestおよびguard ref-change判定から除外してください。
```text
refs/codex/turn-diffs/*
refs/codex/snapshots/*
```

`refs/codex/*` 全体を除外してはいけません。

Desktop内部で自動生成・世代交代することが確認できた上記namespaceだけを対象としてください。

HEAD、index、worktree、およびrepositoryがauthorityとして実際に利用するrefの検証は維持してください。

## 必須regression test

少なくとも以下を追加してください。

### 1. turn-diffs追加

guard snapshot取得後に
```text
refs/codex/turn-diffs/*
```

が追加されてもguard failureにならない。

### 2. turn-diffs削除・更新

同namespaceのrefが削除またはOID変更されてもguard failureにならない。

### 3. snapshots変更
```text
refs/codex/snapshots/*
```

の追加・削除・更新でもguard failureにならない。

### 4. repository authority ref

Desktop volatile namespaceではない監視対象refが変更された場合は、従来どおりguard failureになる。

### 5. HEAD / index / worktree

これらの保護は従来どおり機能する。

## recovery loopの再発防止

同一guard failureについて、同じ復元処理を何度も反復してはなりません。

差分が上記Desktop volatile namespaceだけであることを機械的に確認できた場合は、それをauthority mutationとして扱わず処理を継続してください。

volatile namespace以外の差分が存在する場合だけ、既存の正規guard recoveryを使用してください。

「過去digestに戻るまで探索を続ける」という処理を作ってはいけません。

## 現在taskの継続

このguard修正とtestを完了したら、保存済みの現在taskの実装差分を維持したまま正規経路から現在taskへ戻り、開発作業を継続してください。

現在taskを破棄しないでください。\
NEXTへ飛ばないでください。\
新しいtaskを勝手に作らないでください。

この修正のためにユーザーへ新しいpermissionの文言を要求して作業停止してはいけません。

実行基盤上で通常の技術的手続が必要なら既存authorityの範囲で実行してください。

本当にhost execution layerそのものが操作を拒否した場合は、その拒否をuser permission不足へ変換せず、既存の技術的実行経路で処理を継続してください。

## トークン浪費防止

ここから先、原因調査だけを目的とした追加探索は禁止します。

原因は既に実装修正に十分な粒度まで特定されています。

次の作業は調査ではなく、
```text
guard source修正
→ regression test
→ validation
→ 現在task resume
→ 現在taskの実装作業
```

です。

過去のDesktop内部refを復元することは成果物ではありません。

開発作業を進めてください。
````

## Resolved references

- 2026-09-06、task `1c279537-b51f-4c33-b89a-3309408390ea`のexplicit-fixは実行環境のPATHに`claude`がなくexit 127で終端した
- recovery handoffは`task_status=active`・`required_action=none`・`last_material.outcome=error`を返した
- 契約に従ってread-only `glm-worker --watch`へattachしたが、model processが存在しないまま約23分終端せず、ユーザー中断が必要になった

## Purpose

terminal error後に実行processが存在しないactive stateへ`--watch`した際の無期限待機を機械的に防ぎ、Codexの不要な停止と手動介入を削減する。

## External feasibility

status: not-applicable

## Contract

- `--watch`は対象repositoryのtask/liveness/stateを機械判定し、実行processへattach可能な場合だけ待機する
- activeでも最新material outcomeがterminal errorでrepository lockがfreeかつlive ownerがない状態は、待機せずbounded machine errorまたは正規resume actionを返す
- raceがある場合は誤ってrunning processをstale扱いせず、既存repository lockとtask livenessを正として収束する
- handoff recoveryとwatchが同じ状態を矛盾して表現せず、親が自由言語でorphan判定しない

## Must not

- 経過時間だけのtimeoutで正常な長時間model処理を中断しない
- global process一覧や別repositoryのprocessを生存判定に使わない
- active stateを自動reset・deleteしない
- error後のworker/reviewer sessionとresume checkpointを破棄しない

## Acceptance criteria

- active・latest outcome error・repository lock free・live ownerなしfixtureで`--watch`がmodel call 0回かつbounded時間で終端する
- activeでlive ownerありfixtureでは既存watch attachを維持する
- lock取得/解放raceとstale ownerのtestがある
- terminal resultがcanonical next actionまたはrecovery locatorをmachine-readableに返す
- Codexが無期限blocking waitから手動中断する必要がないことをintegration testで固定する
- independent reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- 意味のある状態遷移がない通常のrunning処理は最大blocking waitで待ち、liveness報告のために親へ戻らない
- GLM worker/reviewerにGit remote write authorityを付与しない

## Dependencies

none

## Review findings

none

## Current boundary

未着手。2026-09-06のqsurface explicit-fix transport error後にproduction相当の`--watch`で再現した。
