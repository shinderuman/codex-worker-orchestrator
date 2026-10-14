# Task: 自己確認型テストを実動作ベースへ再設計

## Original instruction

2026-10-14 ユーザー原文（この要求を三つの独立Taskへ責務分割したもの）:

````text
とりあえず最終的に今回のPRはそのままMergeしていいということでいいか
それで問題なければ別のPRを作ってくれ
Dogfood改善のTaskの次に今回調べたテストをするテストを改善する修正のタスクだ
テスト自体を再設計するものと、再び同様の機構を入れないための機械的ハーネスを入れるタスク、それ以外にやることあればそれも入れてくれ
````

## Amendments

none

## Resolved references

- 「今回のPR」: PR #1350。品質ツールをGoテストジョブへ導入するCI修正。2026-10-14にSquash Merge済み。今回の3 TaskはこのPRの実装変更をやり直すものではない。
- 「Dogfood改善のTask」: 現行Planの `IMPLEMENTATION_TASKS/dogfood-bundle-three-task-audit.md`。3 TaskをそのNEXT直後に連続登録する。これは優先順位でありhard dependencyではない。
- 「今回調べたテストをするテスト」: PR #4で削除された類型、および当時の調査で現行mainから確認した、自身のテスト関数・JSON・Markdown・YAML・Shell・Go sourceの存在・文言・定義値を固定するだけの検査。機能・権限・安全境界の実際の挙動や結果を検証しているかを問う。名称や文字列チェックだけで無効とは断定しない。
- 過去の近接Issue: #1189（冗長テスト増殖防止の機械的ガード、close）、#1032/#1280/#1138（重複enforcementの監査）。履歴・重複判断の参照であり、現在の実装権限は実行時Gitを正とする。

## Purpose

既存のテスト群から、テストや定義・指示文書が「書いてあること」だけを確認する循環的・自明な検証を取り除き、必要な不変条件を実際のproduction behavior、公開インターフェース、正規validator・integration経路で検証する。検証対象の意味を失わず、不要なテスト維持・review・Codex読取負荷を減らす。

## External feasibility

status: not-applicable

## Contract

- 実行時のcurrent Gitを対象に、repositoryのGo `*_test.go`、fixture/corpus、instruction/CI/YAML/JSONピンを横断してinventoryする。下記は既知の入口であり、対象をこれらだけに限定しない。
  - `glm-worker/internal/harnesslint/ci_workflow_test.go`
  - `glm-worker/internal/harnesslint/{execution_permission,authority_bootstrap,parent_handoff,task_attribution,quality_surface_approval,glm_wait}_instruction_test.go`
  - `glm-worker/internal/harnesslint/{staged_parent_action_no_reread,web_gpt_autofix_workflow,web_gpt_mailbox_workflow,web_gpt_validation_workflow,web_gpt_validation_failure_contract,external_review_contract}_test.go`
  - `glm-worker/internal/parentaction/no_reread_regression_test.go`
  - `glm-worker/internal/workflow/{packet_precheck_prompt,markdown_readgraph}_test.go`
  - `glm-worker/internal/packet/contract_test.go` と周辺Schema/Registry照合テスト
- 各候補を(a)実動作/結果検証、(b)意義のある静的policy・権限/デプロイ境界、(c)意味を固定しないテキスト/定義値の自己確認、(d)冗長・二重authority、(e)追加調査が必要、に分類し、KEEP / REPLACE / CONSOLIDATE / REMOVEを現在の根拠で判断する。単なる `strings.Contains` や定数比較の存在だけでREMOVE判定しない。
- 本来の安全不変条件がある場合、テスト対象の書式を固定するのではなく、当該production owner、canonical parser・validator、GitHub Actionsの意味、実コマンド/出力、状態遷移などを駆動して検証する。静的検証しかできない場合も、構文・AST・typed schemaなどで意味を扱えるかを評価する。
- 一度確認した意味上のinvariantを別々のfixture/expected JSON/テスト関数へ転記せず、単一canonical ownerに対する独立の観測結果と負例を使う。自己確認だけを目的とするシナリオ・テスト・manifestの重複も解消する。
- テストの価値が証明できない場合は、実際に何の退行を検出できるかを判定し、無意味なものは削除または既存有効テストへ統合する。実行すべきproduction behaviorが不在なら、テストのためだけに新機構を発明しない。
- とくに、実 `harnesslint` がControllerのleaseを更新する統合テストのように挙動を実測する検証は、ツール依存があるだけで「テストをするテスト」に分類しない。
- 変更後の回帰では、対象の実装が誤動作したときに該当テストが失敗し、単に説明文言・ファイル名・テスト関数名だけを変えても正しい動作が維持される限り不必要に失敗しないことを確認する。

## Must not

- 文字列検証、静的テスト、構造検査を一律禁止・削除しない。security・capability・permission・fail-closed・canonical workflowの保護を弱めない。
- 「テスト件数減少」「行数削減」だけを成果にしない。新しい巨大テストframework、第二schema、独立manifest、互換fallbackを作らない。
- harnesslint等production lint ownerの再設計は後続 `self-referential-test-enforcement-owner.md` と `self-referential-test-prevention-harness.md` が担当するため、本Taskへ混ぜない。
- 既に閉じた#1189などの同根対応を二重実装しない。実行時の現行コードで再確認する。
- test-only手直しのためにproduction logicを別実装したり、実行不可能なskip/期待値固定テストへ置き換えたりしない。

## Acceptance criteria

1. repository横断のinventoryを、対象の検証目的、正本、効果、採否、重複/既存ownerとともに報告できる。既知例を漏らさない。
2. 確認された無意味な自己確認・冗長テストを整理し、価値のある安全/機能回帰は実行可能な動作または意味検査として残す。
3. テスト関数名・説明文言・同義のレイアウト変更による不必要な失敗が解消し、実際の不正挙動・誤ったSchema/Policyは検出できることを代表fixtureで確認する。
4. 変更したテストは通常のGo tests、Lintおよび関係するCIを通過し、既存の必要な観測・防御・失敗境界を維持する。
5. 実装可能性のない意味判断や別Taskのproduction owner変更は具体的なevidenceと共に後続へ引き継ぎ、テスト側に擬似的な「全検証済み」を残さない。

## Historical invariants

- テストは本物のproduction behavior/外部契約を検証し、名前や文字列の存在を仕様達成と混同しない。
- fail-closed、Guard、metadata/Git/Task authority、独立のtrust boundaryの回帰保証は保持する。
- Codex ReductionとQuality Deltaの両方を評価し、不要なテスト増殖・モデル可視負荷を削減する。

## Dependencies

none
