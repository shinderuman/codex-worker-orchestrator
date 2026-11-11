# Task: source import の全ツリー反復走査を削減

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1480#discussion_r4448267496

** Minor** · **Performance & Scalability** · **Quick win**

**Quadratic scan runs on every promotion. Gate `removeSourceTreeReplacedDirectory` with a precomputed directory set.**

`applySourceTreeAddedEntries` iterates the full `after` tree. For each entry it calls `removeSourceTreeReplacedDirectory`. That function calls `sourceTreeReplacedDirectories`, which scans the full `before` tree and builds `path+"/"` on each iteration. The cost is O(|before| × |after|) for every call of `applySourceTreeEntries`, including when nothing is replaced. `applyParentSourceFiles` and `applyPrimaryPromotion` pass full `ls-tree -r` listings. The cost grows quadratically with repository size.

`sourceTreeReplacedDirectories` returns a non-empty set only when `path` is a directory in `before`. Compute `sourceTreeDirectories(before)` once and skip the call for all other paths.

`sourceTreeReplacementDirectoryPresent` (Line 159) and `sourceTreeAncestorReplacementPresent` (Line 160) scan `after` once per removed entry. This is the same pattern. Mass deletions hit it. Consider a set lookup there too.

<details>
<summary>⚡ Proposed fix</summary>
<dl>
<dd>

```diff
--- "a/glm-worker/internal/controller/parent_source_import_files.go"
+++ "b/glm-worker/internal/controller/parent_source_import_files.go"
@@ -248,16 +248,19 @@
 	return known[path].path != "" && (entry.Type().IsRegular() || entry.Type() == os.ModeSymlink)
 }
 
 func applySourceTreeAddedEntries(repo string, previous map[string]workspaceTreeEntry, before, after []workspaceTreeEntry) error {
+	beforeDirectories := sourceTreeDirectories(before)
 	for start := 0; start < len(after); start += workspaceBlobBatchSize {
 		batch := after[start:min(start+workspaceBlobBatchSize, len(after))]
 		prior := make([]workspaceTreeEntry, len(batch))
 		for index, entry := range batch {
 			prior[index] = previous[entry.path]
-			if err := removeSourceTreeReplacedDirectory(repo, entry.path, before); err != nil {
-				return err
+			if beforeDirectories[entry.path] {
+				if err := removeSourceTreeReplacedDirectory(repo, entry.path, before); err != nil {
+					return err
+				}
 			}
 		}
 		if err := applySourceEntryBatch(repo, prior, batch); err != nil {
 			return err
 		}
```
</dd>
</dl>
</details>

出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534774042

**[P2] 全ファイルに対して変更前ツリーを毎回全走査するため、source import が O(N²) に退化しています。** `applySourceTreeAddedEntries` は差分に関係なく `after` の**全エントリ**を列挙し、各エントリで `removeSourceTreeReplacedDirectory(repo, entry.path, before)` を呼びます。この関数は `sourceTreeReplacedDirectories` で `before` の**全エントリ**を調べます。前後とも3万ファイルある通常のツリー更新でも約9億回の prefix 判定が生じ、ファイル→ディレクトリ変換が一件もなくても回避できません。Parent source の採用・再実行が大規模リポジトリで実質停止する性能退行です。`before` から置換候補ディレクトリを一度だけ索引化し、対象 `after` パスだけを照会するようにしてください（少なくとも `previous[entry.path]` が同一の通常エントリでは全走査を避けてください）。
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1480#discussion_r4448267496) — `glm-worker/internal/controller/parent_source_import_files.go`
- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534774042) — `glm-worker/internal/controller/parent_source_import_files.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

変更のないentryでもbeforeを毎回全走査しO(N²)となる。削除側にもafter反復走査がある。

## External feasibility

status: not-applicable

## Contract

- before/afterのdirectory・ancestor照会を1回作る索引へ寄せ、通常entryで全ツリー走査を繰り返さない。既存ownerを改修し並行state/cacheを追加しない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 通常大量entry・mass deletion・file↔directory・nested replacementで同じ結果になり、走査回数/benchmarkから通常更新の二乗走査解消を示す。symlink/no-followとuntracked保護を維持し、秒数をCodex削減率に換算しない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
