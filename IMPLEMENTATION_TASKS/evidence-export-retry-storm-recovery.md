# Task: Task-bound evidence exportの連続失敗と反復要求を解消する

## Original instruction

2026-11-10 ユーザー指示（原文）:

```text
これらのうちAuditしてないのを順次Auditしてくれ
重複があるかもしれないからそれは省いてくれ
Findingは最後にまとめて俺に報告しろ
Draft PRは立てるな
```

先行するFinding処置指示（原文）:

```text
FindingはIMPLEMENTATION_PLANにタスクを追加するPR作るんだよ
Squash Mergeするんだよ
```

## Amendments

none

## Resolved references

- [Audit #1534](https://github.com/shinderuman/codex-worker-orchestrator/issues/1534) の `91643055-01cb-4f2a-8bdb-074d65030baf.zip`、`bound/state/parent-action-attempts.jsonl` に `export-bundle` failure 191件。ただし先行Taskに属する127件（`task_id`なし、Task開始前）と現Taskでの64件を混同しない。
- 現Task `post-pull-mechanical-source-resynchronization.md` 中、2026-11-06 12:11:56〜12:30:08 JSTに `export-bundle` **64件連続 failed/action-failed**、実command duration合計 **約624.7秒**。実Parent transcriptと各stderrが添付ZIPにはなく、同じエラー/同じ入力かまでは証明されない。
- 先行Task側の127件は約34.4秒の合計durationであり、64件の高コスト経路との関連は未確認。Task wallから待機を誤計上したり、全191件を同じTaskの消費としない。
- 既存の証拠出力やBundle/analysis入口、Task/Attempt/episode選択とController generation/lease、完了後read-only exportを利用し、Taskやexport正本の重複導入をしない。

## Purpose

証拠回収が失敗したTaskに対して親Codexが実質同じexportを何度も試し続ける経路を特定し、正規の証拠/再開/停止を維持したまま、原因に応じて反復失敗と時間消費を減らす。

## Contract

- 親のraw tool call/argv/stderr、返却Action errorと状態/Task/Attempt、export内部の失敗点を追跡し、64件をsource・error signature・変更状態の有無で分類する。以前の127件とは別に測定する。
- 終端/未開始/並行実行/invalid Task/lease/未作成artifact等を区別し、なぜ正当なexportが失敗したか、親が再試行する必要があったかを確定する。正常なexportは一回で原本ZIPを回収し、失敗したらactionableなmachine-status/recoveryへ収束できるべきだが、回数制限を恣意的に決めて真の進捗を阻害しない。
- 同一原因が証明された場合のみ既存exportのowner、parent admission、canonical handoff/continuationを修正する。metadata-source、immutable attempt、Task権限、ZIP manifest完整性、必要raw logを維持し、過剰な追加親model turnを避ける。
- 外部ツール制限や人手の不合理な再呼出が原因でmachine gateが成立しない場合は、現実の制約を明示しno-change/既存ownerへの接続を採否判断する。

## Must not

- 成功していないBundleを成功済みと偽る、重要証拠を黙って欠落させる、間違ったTaskのBundleで代替する、source/lease fail-closedを緩めることをしない。
- 親の万能ブラックリストや新daemon、第二state、無意味な固定retry上限、追加AI reviewerを採用しない。
- 正当なユーザー要求・新しい状態遷移・新しい証拠に対する再試行を無差別に禁止しない。

## Acceptance criteria

- 原本の64 failed actionと各実際の原因・retry triggerを再現し、必要だったRetryと無意味な失敗の比率を区別する。原本不足ならunknown/証拠回収範囲を示す。
- repositoryで直す原因が実証されたら、正常Task/Attemptで必要なBundleを安全に回収でき、同一状態・同一失敗への反復が正規handoffで止まることをproduction入口の正負testで検証する。
- 失敗したTask/exportでremote/Task/Sourceを壊さず、missing/unreadableとtyped recoveryを保持する。
- before/afterで失敗回数、command実時間、親turn/usageの可観測値、Quality Deltaを比較する。親token欠測なら削減率を宣言しない。必要なsource-bound validation/CIと独立reviewを通す。

## Historical invariants

- 現行Plan/Rules、source-bound Bundle manifest、GLM親権限分離、失敗時fail-closed、1 Task 1 Commitを維持する。

## Dependencies

none
