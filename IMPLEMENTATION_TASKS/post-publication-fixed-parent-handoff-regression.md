# Task: 修正版親コマンドのPublication後terminal handoff再発を解消する

## Original instruction

2026-11-05 ユーザーが報告した実運用結果（原文）:

```text
前回の handoff 障害は、修正版の親コマンドでも再現しました。今回は公開済み OID と読み取り専用の復旧コマンドが返り、再公開せずに停止状態を確認できました。handoff 障害自体は未解決です。
```

2026-11-05 Findingの実装計画への登録指示（原文）:

```text
FindingはIMPLEMENTATION_PLANにタスクを追加するPR作るんだよ
Squash Mergeするんだよ
```

## Amendments

none

## Resolved references

- 「前回のhandoff障害」は退役済み`IMPLEMENTATION_TASKS/post-publication-terminal-handoff-scope-failure.md`に対応する。旧修正の公開OIDは`0bd5500e43a761af62b5411bf56e40c160c84921`。以前の自然実行では修正版のinstall中に旧親プロセスが動作していたため、修正版の実効性は未検証だった。
- 「今回」は`IMPLEMENTATION_TASKS/darwin-tagged-acceptance-before-publication.md`の正規Publication。公開OIDは`04356b1c6387acb40bf71b156106893566a0db77`、対象Bundleは`ba1b9126-b2d6-4d74-90c4-ad9cc0b9a9cc.zip`。Task complete後に最終`controller-publication`親actionがfailedとなったことはbound lifecycleに残る。公開済みOIDと読み取り専用recoveryで再公開せず停止を確認できたこと、および修正版親コマンドでもhandoff障害が再発したことはユーザーの実運用報告。Bundle単体には親stderr・terminal envelope・親実行binaryの完全な原本がなく、**正確な失敗文字列・read-scope変化の原因は未確定**。
- 現行の調査ownerは`glm-worker/internal/parentactioncmd/completion_handoff.go`、`completion_workflow.go`、`terminal_envelope.go`、`glm-worker/internal/parentevidence/support.go`とControllerのPublication/terminal連携を出発点とする。現在のsourceと実ログで責務を確認し、単に旧修正のパッチを再適用しない。
- 正常終了後の親による任意の追加再検証を抑制する別Task`IMPLEMENTATION_TASKS/post-publication-parent-reverification-containment.md`とは異なり、本件は**通常の必須terminal handoff自体が失敗する**問題である。正常時の後続Taskのactionを公開済みTaskに提示するかという別の論点とも混同しない。

## Purpose

修正版親コマンドの正規Publication完了後にterminal handoffが失敗し、Codexが追加の読み取り専用復旧を要する障害を原因に基づいて解消する。Git公開成功とterminal handoff成功を分けたまま、正常経路を一度の親actionで終端まで完了可能にする。

## External feasibility

status: not-applicable

## Contract

- 実行された修正版親コマンドのbinary/source identityを確認し、成功済みPublication、Task complete、handoffのcapture/validate、実際の失敗、recoveryの順序を一次証拠と現行sourceで再構成する。前回の旧binary事例と今回の修正版再発を区別し、同じ失敗文字列・同じ根本原因だと推定で確定しない。
- terminal handoff失敗の直接原因と成立条件（Task status/lease、Controller primary/source、read scopeの取得・検証、同時遷移、I/O等）を再現し、実際のownerを修正する。正規のTask completeやPublicationによる合法な状態遷移はhandoffの偽の矛盾にしない。一方で真の不一致・source失効・破損はfail closedする。
- 正常終了時には同一Publicationのsource-boundなfinal/remote OIDとcanonical handoffを返し、NEXTを開始せず一度の親actionで完結する。publication済みだがhandoff不成立の場合は**既に公開済み**であることを維持したtyped failure・read-only recoveryへ進め、再Publication・二重Pushをしない。
- 現行のintegration/command入口で正常、status/lease変化、source不一致、handoff I/O failure、recovery、再実行を検証する。合成testだけで実運用改善を証明せず、install後の通常実行の証拠は別途追跡可能にする。

## Must not

- handoff障害の隠蔽、成功への誤変換、read-scope guardの無条件緩和、真のsource不一致の容認をしない。
- 完了済みTaskの再実行、remote再公開、二重Push、NEXTの無許可開始をしない。
- 新daemon、第二state、後方互換fallback、追加の汎用Guard、無関係なPublication最適化・親の任意再検証の修正を導入しない。
- 本Taskの登録だけをもって障害解消または既存修正の実効性を証明したことにしない。

## Acceptance criteria

- 修正版親コマンドでの再発について、正規経路→実際のterminal/handoff failure→read-only recoveryの原本とsource identity、直接原因を明示する。原本不足の断定はしない。
- source-boundなPublication成功後、terminalはfinal=remote OID、整合したcanonical handoff、NEXT非起動を含んで正常終了し、復旧コマンド不要であることを正負のproduction経路テストで検証する。
- 真のsource/status/lease/remote不一致、I/O障害ではfail-closedなtyped failureと安全なread-only recoveryが残り、Publication済みなら再Pushされないことを検証する。
- 同一sourceで対象Go test/race、必要なDocker/CI、installとinstalled-state smokeを完了する。後続の通常Publicationでlive効果が未観測ならその限界を明示し、合成PASSをlive証明にしない。
- 既存ownerへの限定修正、独立review、1 Task 1 Commit、通常のTask終了Metadata同期を守る。

## Historical invariants

- 現行Git、`IMPLEMENTATION_RULES.md`、Plan、このTaskを正本とする。旧Taskの完了は履歴として保持し、管理Issueを第二の実装要求正本にしない。
- 適切なsource identity・権限・Guard・正規RecoveryとCodexの最終判断を維持する。

## Dependencies

none
