# Task: テスト存在照合・Provenanceと静的Guardの責務を再設計

## Original instruction

2026-10-14 ユーザー原文（この要求を三つの独立Taskへ責務分割したもの）:

````text
とりあえず最終的に今回のPRはそのままMergeしていいということでいいか
それで問題なければ別のPRを作ってくれ
Dogfood改善のTaskの次に今回調べたテストをするテストを改善する修正のタスクだ
テスト自体を再設計するものと、再び同様の機構を入れないための機械的ハーネスを入れるタスク、それ以外にやることあればそれも入れてくれ
````

## Amendments

### 2026-10-25 External ReviewのCodex採否によるrename境界の明確化

親Codex指示:

````text
PR #1353 discussion_r4282514693について、単純renameの受理はstale locatorを無条件に通すという意味ではない。存在確認を現行の正規契約として残す間はcoordinated renameで登録locatorも更新する。locatorの責務自体を変更する場合はconsumerを確認し、既存の不正provenance/missing symbolに対するfail-closedを維持する。coverageの意味保証とは区別する。
````

## Resolved references

- 「今回のPR」: PR #1350。品質ツールをGoテストジョブへ導入するCI修正。2026-10-14にSquash Merge済み。今回の3 TaskはこのPRの実装変更をやり直すものではない。
- 「Dogfood改善のTask」: 現行Planの `IMPLEMENTATION_TASKS/dogfood-bundle-three-task-audit.md`。3 TaskをそのNEXT直後に連続登録する。これは優先順位でありhard dependencyではない。
- 「今回調べたテストをするテスト」: PR #4で削除された類型、および当時の調査で現行mainから確認した、自身のテスト関数・JSON・Markdown・YAML・Shell・Go sourceの存在・文言・定義値を固定するだけの検査。機能・権限・安全境界の実際の挙動や結果を検証しているかを問う。名称や文字列チェックだけで無効とは断定しない。
- 過去の近接Issue: #1189（冗長テスト増殖防止の機械的ガード、close）、#1032/#1280/#1138（重複enforcementの監査）。履歴・重複判断の参照であり、現在の実装権限は実行時Gitを正とする。

## Purpose

production側の品質検証が「テスト関数が定義されていること」や「Go/Markdownに文字列があること」を実挙動の保証と誤認する構造を整理する。Provenance/機械制御の追跡可能性と安全な静的Guardを保持し、意味のない二重正本と形式固定を削減する。

## External feasibility

status: not-applicable

## Contract

- 実行時のcurrent Gitにおける `glm-worker/internal/harnesslint/control_provenance.go`、`control_projection.go`、`quality_surface.go`、`go_rules_test_structure.go`、`codex/control-provenance.json` と周辺consumer/テストを調査する。これは存在確認型機構を持つ既知entrypointであり、他の同種production ownerにも対象を広げて確認する。
- `validateControlProvenanceLocator` / `goFileDefinesSymbol` は登録された `*_test.go` に関数が存在するかをASTで確認するが、空のTest関数でも通過する。これを「テストが存在する」というlocatorの妥当性確認と「不変条件が検証される」というcoverage保証に明確に分離する。
- Provenance locatorが単なるnavigation / control ownership参照として必要か、fail-closedで保護すべきschema/symbolリンクかを検討し、source of truthと利用者を確認する。存在確認を残す場合は、それが実動作保証でないことを機械結果・説明・使用箇所に反映する。実検証に代わる別の自己確認Testを加えない。
- `qualityWiringViolations` 等によるsource token/order pinを棚卸しし、production safetyに実際に必要なdeterministic predicateと、実装の記述形式を偶然固定するだけのpredicateを分ける。残す意味検査は可能な限りcanonical owner・構文/AST/typed contractを利用し、同義変更を不必要にrejectしない。
- 既存のprojection/provenance registry、CI、prompt、hook、lint、testの間で同じinvariantを異なるauthorityが重複定義していないか照合し、canonical predicateを共有できるものは共有する。ただし異なるtrust/freshness boundaryの独立チェックは維持する。
- 本Taskで必要なproduction owner修正と回帰テストだけを実施し、`self-referential-test-redesign.md` の通常テスト整理や `self-referential-test-prevention-harness.md` の新しいanti-pattern検出機構は実装しない。

## Must not

- 不正なprovenance、missing owner、missing path、malformed schema、危険なprompt/permission driftをfail-openへ変えない。
- 新たなtestの存在チェックやテスト定義の二重固定を「coverage強化」として追加しない。
- 関数名固定を減らすためにtest coverage自体を削らない。既存のprovenance参照が外部やCIで使われている場合、そのconsumerと影響を調査せず削除しない。
- regex/文字列一致を意味検証済みと表現しない。意味を機械判定できない場合は正直に残余のsemantic判断として扱う。
- 自己確認型validatorを汎用plugin/登録frameworkへ拡張せず、既存ownerの範囲で整理する。旧schema/fallbackを追加しない。

## Acceptance criteria

1. test locator、canonical test、postconditionの現行consumerを列挙し、存在確認とbehavioral coverageが区別されている。
2. 実態に根拠のある冗長・循環・テキスト形式依存のproduction GuardをKEEP/REPLACE/REMOVEのいずれかに整理し、採否が不変条件単位で説明できる。
3. 不正provenance/権限境界/機械制御の回帰は引き続き失敗し、有効な同義のsource changeやtest関数の単純renameが無関係な「保証違反」にならないことを実証する。
4. 既存のlint、Go tests、必要なCLI/CI・integration checkを通し、Guardのscope/信頼境界を緩めない。
5. 新規anti-pattern予防guardへ引き渡すべき具体的なsyntax/AST例とfalse-positive条件を evidenceとして記録する。

## Historical invariants

- 検査可能な構造上の事実と、テストが本当に有効であるという意味判断を混同しない。
- 複数trust boundaryの実効的なdefense-in-depthとfail-closedを維持する。
- generic mechanismと本repository固有policyを別責務として保つ。

## Dependencies

none
