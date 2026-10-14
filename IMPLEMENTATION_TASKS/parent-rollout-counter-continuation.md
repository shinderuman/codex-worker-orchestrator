# Task: 親rolloutの累積counter継続と証拠関連付けの修復

## Original instruction

ユーザー原文:

````text
いまの修正状況は割と切りが良いところではあるのか？
であれば一度ここでコミットしてPushしろ
それを妨げる場合はこの場限りの特例を許す
その不具合は次のActiveに積め
````

## Amendments

## Resolved references

- 「いまの修正」は `IMPLEMENTATION_TASKS/execution-worktree-architecture-review.md` の実装成果を指す。「その不具合」は、復旧した親チャットのrollout segmentが前segmentの累積token counterを継承し、既存ownerがself-contained counter再開始を要求してcandidate accept/sealを拒否した問題を指す。
- このTask登録と同じGitコミットに保存した実装・Metadataは通常Publication・Retire成功の証拠ではない。Worktree Taskは未達Acceptanceを保持する。
- 正規ownerの調査locator: `glm-worker/internal/codexrollout/chain.go` の `ResolveChain`、`counterBoundaryReason`、`counterRestarted` と `glm-worker/internal/controller/runtime_evidence_sessions.go` の `runtimeParentRolloutChain`。集計consumerへの影響は未確認であり、条件緩和だけで安全と推定しない。
- immutable source archive: `94f869d6d363c235d4b002a91601f8c931750b43fd17e62fcba6420ead3af066`。
- Review・Validation・install evidence archive: `0ffb7a3eeb5b774c3d2ffcfa1e28a2d2f51d69ea516541cf26510cb5423adde8`。
- STOP evidence archive: `0935502000233c8fa1fbdfc17d3a121abe45c40fc5b356d3b9d97941d6093e61`。manifestと境界診断を含む。これらは既存Controller immutable storeの参照であり、raw会話をMarkdownへ複製しない。

## Purpose

親チャット復旧時の累積counter継続を既存のrollout・Runtime証拠ownerで正しく扱い、証拠関連付けと消費量の正確性を保った通常Lifecycleを再び利用可能にする。

## External feasibility

status: not-applicable

## Contract

- current Git、canonical bootstrap、Controller、Runtime、recovery handoffを再確認し、登録時のgenerationやsnapshotを操作前提にしない。
- PlanのACTIVE変更、例外保存後のGit、保持されたWorktree Task authorityを区別する。新ACTIVEへの束縛は既存ownerの正規transitionで成立させる。既存成果・identity・証拠を保全し、Task中途切替を成功Retireへ変換しない。
- 正規bootstrapが旧authorityを返す、または拒否する場合、原因診断に限ってprimary tracked source・Metadataと保存Controller／Runtimeをread-onlyで参照する。これをbootstrap成功の代用にせず、修復後は正規bootstrapとhandoffへ戻る。
- 同一親sessionのsegment境界、timestamp、cwd/source/originator、counter全field、重複排除、集計consumerを照合し、counter再開始と継続を区別する。継続を認める正確な契約と不正・曖昧境界の拒否条件は実装前にSol判断へ通す。
- currentデータに対する契約を既存ownerへ実装する。継続counterを重複加算せず、親チャット全体の累積値を今回Task消費として扱わない。証拠不足はunknownとして保持する。
- 例外的Git保存後のsource・公開OID・Controller authorityの関係を正規ownerで確認する。既にPushした保存コミットを書き換えず、当該保存をController Publication証拠へ偽装しない。
- 修復をReview、必要なValidation、Lint、build、installと本配置一致確認へ接続する。Worktree Taskの未達通常Lifecycle検証は同Taskへ残し、修復後に正規再開できるhandoffを用意する。

## Must not

- Counter guardの単純無効化、必須証拠のoptional化、counter reset、対象segment削除、rollout捏造、新sessionへの逃避をしない。
- State、terminal record、checkpoint、Controller headを直接書き換えない。既存受理済み成果・immutable evidence・one-shot消費を変更しない。
- 旧Taskを復活しない。Worktree Taskの実装・第1milestone・消費済みresubmit-reportを再実行しない。
- 旧Schema対応、migration、dual protocol、互換fallback、第二Controllerを追加しない。
- 今回のGit特例を恒久化しない。GLMにcommit/pushその他remote writeを行わせない。
- CLI Task、Bundle再監査、効率化の別Taskを実装しない。

## Acceptance criteria

- 同一sessionの確認可能な継続counterが正しく関連付けられ、再開始segmentと継続segmentの消費集計が重複しない。
- 別session、不一致identity、時間範囲重複、重複content、欠損・曖昧counter、正当性を確認できない境界を従来のfail closed方針で拒否する。
- 継続・再開始・不正境界・下流consumerをモデル実呼出しなしの回帰で固定する。
- 保存された失敗証拠と実segmentを使い、parent rollout associationが正規ownerで成立することを確認する。例外commit/pushを正常Lifecycle成功に読み替えない。
- ACTIVEとController／Runtimeのauthorityが正規に接続され、同一snapshotのbootstrapとconsistentなhandoffが合法actionを提示する。
- GLM独立Review、適切なGo tests、build、vet、最終 `./harnesslint` PASS（違反0）、必要なinstall-smokeとsource／本配置一致を確認する。
- Worktree Taskの残Acceptanceを失わず、保存成果から正規再開可能である。通常Publication／Retireの完了は実証した場合だけ報告する。

## Review findings

- Worktree Taskのcandidate accept/sealで `required parent rollout association is ambiguous: rollout chain boundary does not restart a self-contained token counter` が返った。前segmentの最終counterと復旧segmentの最初counterは全usage fieldが一致し、同じ親session IDを保持している。既存判定はこの継続を再開始として認めない。下流集計を含む修正契約は未確定。

## Historical invariants

- Codex Reductionと意味判断の品質を維持し、機械的な既存証拠・委譲・復帰経路を正とする。
- Git保存、親のReview受理、Controller semantic acceptance、Publication、Retireを別の成立事実として扱う。

## Dependencies

none
