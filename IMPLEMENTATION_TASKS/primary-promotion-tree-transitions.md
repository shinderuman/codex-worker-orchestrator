# Task: Primary promotionのtree構造変更

## Original instruction

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
file/directory置換とsymlink変更をarchived candidateから安全に適用する。
old/current/targetの所有を先に検証し、file→directory・directory→fileで競合する正当な旧pathを削除してからtargetを適用する。共有source-tree writerを正規ownerに保つ。
mode120000のlink blobをfollowせずに再現し、通常file・実行file・削除とのmode変更を扱う。ancestor traversal・Git管理path・無関係mutationの拒否を維持する。
各適用境界での中断後、beforeまたは許可targetだけを受理して同じjournalから再試行する。未帰属pathを消して通過させない。
````

## Amendments

none

## Resolved references

- PR #1391 / comment 4370304599: 新childのancestorを旧file状態で検査して拒否し、writerも追加から先に実行する。file/directory置換の検証と適用順・中断再試行を同じownerで扱う。
- PR #1391 / comment 4370304600: execution materializerは120000を扱うがprimary validator/writerは新変更symlinkを拒否する。Git blobのlink targetをfollowせず再現し、unknown mutation/path traversalを拒否する。directory置換と同じtree適用ownerへまとめる。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/controller/publication_primary.go:validatePrimaryPromotionEntry`
- `glm-worker/internal/controller/parent_source_import_files.go:applySourceTreeEntries`
- `glm-worker/internal/controller/suspension_trees.go:suspensionWorktreePath`
- `glm-worker/internal/controller/execution_materialize.go:materializeExecutionEntry`
- `glm-worker/internal/controller/parent_source_import_files.go:applyParentSourceEntry`

## Purpose

file/directory置換とsymlink変更をarchived candidateから安全に適用する。

## External feasibility

status: not-applicable

## Contract

- old/current/targetの所有を先に検証し、file→directory・directory→fileで競合する正当な旧pathを削除してからtargetを適用する。共有source-tree writerを正規ownerに保つ。
- mode120000のlink blobをfollowせずに再現し、通常file・実行file・削除とのmode変更を扱う。ancestor traversal・Git管理path・無関係mutationの拒否を維持する。
- 各適用境界での中断後、beforeまたは許可targetだけを受理して同じjournalから再試行する。未帰属pathを消して通過させない。

## Must not

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

- 両方向のfile/directory置換とsymlink追加・target変更・fileとのmode変更が通常accept/promotion経路で成立する。
- partial applyの再試行と第三者path/mutation拒否を確認する。
- 最終primary worktree/indexは受理treeと一致し、linkをdereferenceして外部を書き換えない。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

## Historical invariants

- Current Gitと本Taskを要求正本とし、外部Review/Issueを第二仕様にしない。
- 誤成功・未帰属状態のadopt・既存成果の破壊で復旧させない。

## Dependencies

none
