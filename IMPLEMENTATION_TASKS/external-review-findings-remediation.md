# Task: External Review #1401の指摘をCodexが評価し、必要な修正を完遂する

## Original instruction

2026-10-27 ユーザー指示（原文）:

````text
[https://github.com/shinderuman/codex-worker-orchestrator/pull/1401](https://github.com/shinderuman/codex-worker-orchestrator/pull/1401)

このExternal Reviewの指摘内容に対応するというタスクをIMPLEMENTATION_PLANに作成してPR作っておいて
お前自身がExternal Reviewの内容を見る必要はない
指摘内容に対応するのはCodex自身だから
````

## Amendments

none

## Resolved references

- 「このExternal Review」は https://github.com/shinderuman/codex-worker-orchestrator/pull/1401 を指す。指摘の一次資料はこのPRの本文・差分・review/comment等、GitHub上の当該Reviewの現物とする。
- 本Task作成者は指摘内容を査読・採否判断していない。具体的なFinding、重大度、解決済みかどうかを推定・事前確定しない。親CodexがTask開始時に現在のGitとPR #1401を読み、レビュー対象sourceとの差異を把握して評価する。
- PR #1401は修正対象を指す外部Reviewのlocatorであり、そのbranchや差分を無条件にmerge/cherry-pickする指示ではない。正規の実装authorityは現在のGit・Rules・Plan・本Taskにある。

## Purpose

External Review PR #1401の各指摘を親Codex自身が検証・採否判断し、実在する問題を正規Workflowで修正して終結させる。単にレビュー本文をTaskへ転載したり、指摘を無批判に受け入れたりしただけで完了にしない。

## External feasibility

status: not-applicable

## Contract

- 親Codexが先に現在のGit・関連Task/PR・External Review #1401の原本（本文、関連差分、review thread、comments、参照source）を確認する。review対象revisionと現在の修正対象revisionを区別し、後続commitによる解消・重複・前提の失効を判定する。
- Reviewが提示した個々の指摘について、主張、検証対象、実際の挙動・再現性、影響、既存owner、既存Taskでの対応状況をcurrent sourceと根拠付きで照合し、採用して修正するものと、既解消・重複・誤検出・対象外等により修正不要とするものを親Codexのsemantic authorityで決定する。
- 採用した指摘は、症状だけでなく原因と必要な不変条件を確認してproduction code・test・証拠を適切に変更する。単一責務で収まるものは同一Taskで対応する。独立の大規模変更や他のownerに属する内容は、問題を放置して完了扱いするのではなく、既存Taskとの重複や依存を確認したうえで要求・受入条件をlosslessに追跡可能な独立Taskへ分離し、Plan上の順序を親が判断する。
- 各指摘の最終dispositionをPR #1401の原文位置または具体的参照と結び付けて、修正commit/test証拠、既解消の根拠、No-Goの判断理由、別Taskへの正確な引継ぎ先を区別できる形で既存のreview/Task artifactへ残す。後から対象Reviewで指摘の扱いを追跡できるようにする。通常の作業日記をtracked Markdownへ複製しない。
- 実装・回帰テスト・関連する正規Quality Gate、独立Reviewer、親Codexの最終採否、Publication/Retireは既存の正規Workflowに従う。必要なvalidationの宣言と実実行・source bindingを照合し、欠落をPASSと見なさない。

## Must not

- 本Taskを作成したWeb GPTによる指摘内容の評価を既成事実としない。CodexがPR #1401を読まずにレビュー完了を宣言しない。
- External Reviewの記載を無条件に採用しない一方、再現した真のFindingを検証不足・軽微扱い・一律No-Goで放置しない。
- 見かけだけの修正、テストやGuardの弱体化、失敗のskip/timeout延長、根拠のない新規fallback・後方互換、無関係なリファクタリングを行わない。
- PR #1401そのものを無条件にMergeしない。Task要求の一次資料をコメントではなくTask fileに保持し、既存のACTIVEをこのTask作成だけで変更しない。

## Acceptance criteria

- PR #1401のReview全指摘を親Codexが現物から列挙し、current revisionに対する採否と根拠・実装責務を漏れなく確定する。確認できない指摘は「指摘なし」に読み替えず、資料欠落やunknownとして扱う。
- 実在すると採用したFindingは、再現・原因の特定と必要なproduction修正/回帰テストが完了するか、別の具体的なtracking Taskへ責務・優先度・受入条件を損失なく引き継ぐ。既解消/重複/誤検出は正確なcurrent Git/テスト等の根拠を示す。
- 対象Review内の各指摘について、指摘→判断→修正/不要理由/引継ぎ先→検証結果を追跡できる。採用対象に未対応が残るまま全Finding解決と報告しない。
- 変更した挙動の主要な正負例、必要な正規validation・独立review・source整合、publicationの成立を確認する。変更不要との判断だけで終える場合も全指摘の採否証拠を保存する。

## Historical invariants

- 原本のExternal Review #1401、current Git、Rules、Plan、本Taskを照合し、Codex自身のsemantic判断とGLM実装・独立Reviewの責務を維持する。
- 正規Lifecycle、Git authority、1 Task 1 Commit、source-bound evidence、fail closed、後方互換性禁止を維持する。

## Dependencies

none
