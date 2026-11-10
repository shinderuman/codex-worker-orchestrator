# Task: Publication後に親terminal actionが失敗し続ける実経路を解消する

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

- [Audit #1532](https://github.com/shinderuman/codex-worker-orchestrator/issues/1532): Task `parent-inflight-wait-admission.md`、正常なPublication操作の後に同Taskの `controller-publication` action failed、87.724s。
- [Audit #1533](https://github.com/shinderuman/codex-worker-orchestrator/issues/1533): 前回のhandoff修正を実装した `post-publication-fixed-parent-handoff-regression.md`（公開 `1a63fcb4ba21c8daac932b86cde869c3309f54c9`）自身の最終親 `controller-publication` は `failed/action-failed`、262.718s。
- [Audit #1534](https://github.com/shinderuman/codex-worker-orchestrator/issues/1534): `post-pull-mechanical-source-resynchronization.md`中に同action failed 3回 / success 1回。
- [Audit #1535](https://github.com/shinderuman/codex-worker-orchestrator/issues/1535): `post-install-probe-bundle-evidence-fidelity.md`中に同action failed 256.168s。
- [Audit #1539](https://github.com/shinderuman/codex-worker-orchestrator/issues/1539): 後続 `communication-transport-unification.md`では同actionの正常終了を実観測。**一度の後続成功は先の失敗原因の恒久解消を証明しない**。
- 修正済みTask `post-publication-terminal-handoff-scope-failure.md` / `post-publication-fixed-parent-handoff-regression.md` はいずれも過去の完了実装であり、退役状態を巻き戻さない。後者の現実装の実効性と残存異常を調査する新たな作業であり、コード再適用を要求しない。
- Bound action記録は失敗の存在と実行時間を証明するが、親raw stderr、terminal envelope、実際のbinary identityを必ずしも含まない。`action-failed` を確証のない `parent_action_terminal_handoff_failed` と同一視しない。

## Purpose

Publication成功・Task complete・必要なcanonical terminal handoffの三境界を保持しながら、公開済みOIDの後で親`controller-publication`が失敗し、復旧・再探索を要する現象の原因を確定し、同じ正規操作で安全にterminalまで収束させる。

## Contract

- 上記4 Auditの当該Task/Action/Sourceごとに、実行親binary/source identity、terminal結果と具体stderr、Task status/Controller generation/lease/primary/remote、実行後のcanonical handoffと復旧有無を原本から時系列に復元する。実原因が異なるAction failureは別因子として切り分け、後続成功も比較する。
- 原因を既存`glm-worker/internal/parentactioncmd`/Controller/`parentevidence`の適切な単一ownerで修正する。必要なstatus/lease/source/remote protectionを解除したり、失敗を黙ってsuccessful terminalへ変換しない。
- すでにGitへ公開されたTaskでは公開OID・Task完了とtyped recoveryを保持し、repeat commandからの二重Push・重複完了Metadata・NEXT無許可開始を拒否する。まだ未公開なら通常fail-closedを維持する。
- parent-sideの任意の成功後再検証を抑制する別Task`post-publication-parent-reverification-containment.md`を重複実装しない。API entrypointを増やすより既存のtyped正規経路を優先する。
- 現実の失敗が親processの旧version残存や外部I/Oだけなら、それを機械的に識別・復旧できるか採否を判断し、不要なproduction変更を行わない。

## Must not

- 原因不明のaction failedを全て同じhand-off rootへ帰属させない。
- source guardの解除、エラー隠蔽、再Publication、旧schema compatibility、第二daemon/state、不要なGuard新設を行わない。
- 既完了のIssue/Taskを再Openしたり、同一作用を別taskで重複実装しない。

## Acceptance criteria

- 少なくとも各失敗を具体terminal stderr・binary/source identityに結び付け、必要な本修正/外部未解決/既対応/No-Goを判定する。原本欠測ならunknownとし、推定修正を完了宣言しない。
- 問題がrepository実装なら、正規Publication→Task complete→canonical handoff成功、偽failure無し、NEXT非起動をproduction経路で再現・検証する。真のscope/source/remote競合とI/O failureは引き続きtyped failure、read-only recovery可、二重Push無しとなる。
- 変更範囲は原因ownerと正負テストに限定し、同一sourceのGo/race、必要なinstall/smoke、required CIを通す。
- installed後の通常Taskでlive効果が未観測なら未実証と明記し、合成testや一回の成功で恒久解消を主張しない。

## Historical invariants

- `IMPLEMENTATION_RULES.md`、Plan、source-bound canonical Controller/Publication、1 Task 1 Commit、親Codex最終採否、退役Task削除を維持する。

## Dependencies

none
