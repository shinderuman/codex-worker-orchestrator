# Task: 自己確認型テストの再流入を防ぐ機械ハーネス

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

PR #4で削除された類型が後続開発で再流入した事実と、既存 `scenario-self-test` / `test-shadow-production` / `test-size-limit` の検出範囲の限界を踏まえ、**決定可能で誤検出が許容できる既知の自己確認型テスト/二重正本**だけを既存のmachine harnessで拒否する。品質を保ち、不要な親Codexのreview・反復修正・テスト維持負荷を減らす。

## External feasibility

status: not-applicable

## Contract

- 実行時のcurrent Gitから、既存 `glm-worker/internal/harnesslint/go_rules_test_structure.go`、`quality_surface.go`、既存Go source/test lint、worker-diff validation、`WORKER.md`、過去Issue #1189と本改善系列の修正結果をinventoryする。既に守られている不変条件は再実装しない。
- 再流入した具体例を出発点に、次のような **semantic-neutralかつ機械的に判定可能なclass** のみを追加・改善する。実際に検出できることを証明してから採用する。
  - 他のtest function / fixtureの存在・命名だけをcoverageの代理とする形
  - static JSON/Markdown等の自己確認だけで「behavior test」と名乗る形
  - test corpusやexpected valuesの二重独立定義による循環的な照合
  - 不要な同型test増殖・production behaviorをテスト側に再実装する形のうち、AST・構文構造でfalse-positiveを制御できるもの
- 既存ハーネスの責務・速度・エラー報告形式に適合させる。canonical producer/consumerへの単一passまたは既存検証surfaceで済むなら、新command、独立scanner、別manifest、model-based semantic reviewerを増やさない。
- 明確な違反は対象ファイル・関数・違反class、残すべきbehavioral ownerへの修正方向を示す。実行上妥当なschema validation、出力の整合性チェック、依存の存在/構文検証、security/capability境界を誤検出しない。
- positive/negative fixturesを用いて、禁止形の実例は落ち、正しい実動作テスト・本物の構造/permission/Schema Guard・適切なtable-driven testは通ることを検証する。ハーネスを検査するために特定文言や関数名の存在を確認するだけの自己参照テストを追加しない。
- 全自動で判別不能な「本当に価値あるテストか」「自然言語の意味が同じか」はmachine enforcedと偽らない。残るhuman/parent/reviewerの判断を限定的に明示し、false-positiveが大きいheuristicは採用しない。
- 既存の`test-shadow-production`、`scenario-self-test` と新規判定を不用意に統合せず、観測されたroot causeに必要な判定だけを追加する。

## Must not

- 「文字列検査をするテストはすべて禁止」などの過大なルールを作らない。全test fileへの万能意味判定、任意の命名規則、LOC/テスト件数quotaを導入しない。
- Go test、CI、`harnesslint`、production observer、state/authority guardの実際の検証範囲を縮退させない。
- 初期の具体例だけに通用するファイルパス一覧の固定や、同一検査を複数validatorへ複製する方法を取らない。
- ルールの存在を保証するための「このルールがsourceに書いてあるか」という自己参照テストを導入しない。
- 別Taskで整理するproduction provenance ownerのテストシンボル照合を無理に改変しない。新たな第二正本、旧Schema、compatibility fallbackを作らない。

## Acceptance criteria

1. 既存の機械guardで漏れる代表的な自己確認/循環/二重正本形について、検出可能性とfalse-positive境界が説明されている。新規machine rule対象外も理由つきで記録できる。
2. 選定したclassの前後fixtureが、実際の既存`harnesslint`実行経路またはworker-diff gateを通って期待どおりPASS/FAILする。
3. 普通のbehavioral regression、独立trust boundaryのstatic/security/schema checks、適切なtable/corpus-drivenテストは拒否されない。
4. 再流入防止の既存ownerへの統合により、余計な追加command・lint layer・second registryが生まれない。
5. full Go tests、`./harnesslint`、必要なCIをPASSし、実際に検出可能な部分とレビュー判断が必要な部分の境界を正確に報告する。

## Historical invariants

- harnessが機械的に強制できるものだけを強制し、実行意味やテスト価値の判定を文字列一致で保証したと主張しない。
- 正規のimplementation / review / validation / Publication / Retireと1 Task 1 Commitを維持する。
- Codex Reductionのための予防ガードが過剰なtest負荷やfalse stopを生まないことを確認する。

## Dependencies

none
