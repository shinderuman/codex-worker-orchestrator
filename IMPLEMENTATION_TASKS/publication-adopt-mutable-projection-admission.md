# Task: 実行できないadopt-mutableを親handoffへ提示しない

## Original instruction

2026-11-01 Dogfood監査で確認した独立Findingを、実装前に正規Plan/Taskへ登録する。

## Resolved references

- Dogfood Bundle `34798183-e40a-42a9-a7bf-b81216f8836c.zip` のTask `IMPLEMENTATION_TASKS/canonical-publication-source-continuity.md`、公開source `84eeb0a6b4e248526e0e6c7c287b422977b8a475`。
- `glm-worker/internal/controller/publication_operation_projection.go:projectAdmissionSourceAdvancement` は、`admissionSourceContinuity` がprimaryのfast-forwardを認めると `adopt-mutable` を次操作として提示する。
- 一方、`glm-worker/internal/controller/publication_mutable_adoption.go:LiveMutableAdvancementAdmission` / `attributeExternalPull` は、同じ操作の実行にremote tip一致、current authority / quiescent lease、未コミット変更との非衝突、lossless baselineを追加で要求する。
- `glm-worker/internal/controller/publication_source_continuity_test.go:TestPulledAdvancementAttributionRefusesUnprovenMutations` は `remote-ahead` と `overlapping-uncommitted` の実行拒否を検証するが、同じ状態の `ProjectParentCompletion` による操作投影は確認していない。
- 実Dogfoodでこの不一致による親側の再実行・停止が起きた証拠はない。現時点のFindingは、現行コードの投影条件と実行前提の差、および同一fixtureでの投影テスト欠落である。

## Amendments

none

## Purpose

親へ提示するtyped next actionと、その操作を実行できる実際のadmission条件を一致させる。正当なPull後のsource再束縛を維持しながら、既に帰属不能と判定できる状態では、実行してから同じ拒否を受ける操作を正規handoffとして案内しない。

## External feasibility

status: not-applicable

## Contract

- `ProjectParentCompletion`、`projectAdmissionSourceAdvancement`、`LiveMutableAdvancementAdmission`、`attributeExternalPull`、および親handoffから操作実行までの既存ownerを調査し、判定の共通化が必要な境界を特定する。
- primaryが正当にfast-forwardしても、remoteがさらに先行している、未コミット変更が同一pathで衝突する、in-flight leaseまたはgeneration/sourceが不一致となる場合は、既知の実行不能な `adopt-mutable` を実行可能な次操作として提示しない。既存の正規rebind、同期、回復操作が合法ならtypedに示し、それが不可能なら理由のあるfail-closedにする。
- 正当なPull後にremote/localとTask/source/baselineが一致している通常系では、既存の `adopt-mutable` とsource-boundなReview/Validationへの復帰を維持する。
- projectionと実行の判定を矛盾した二重実装にしない。必要なremote確認の実時間・権限・失敗時の挙動を観測し、単なる表示のために毎回過剰なremote I/O、モデル再入、別のState/daemonを導入しない。
- 実行側の拒否が必要な場合、親へ返すerrorの原因・次操作を失わないようにする。ただし、正規fail-closedを緩めたり外部変更を推測採用したりしない。

## Must not

- 正規Guardを外したり、remote-ahead/衝突を自動採用したりして操作の実行可能性を偽装しない。
- 非同期的に外部Git状態が変わり得ることを理由に、完全な将来成功保証を要求しない。検証したsnapshotと実行時の再検証は別々の境界として保持する。
- 既存 `canonical-publication-source-continuity.md` で完了した広範囲の再実装、第二のsource authority、一般化したCodex直接実装経路、目的外のPublication高速化を追加しない。
- GLMのトークン削減だけを目的にハーネスを追加しない。既存Taskの独立Review、Go/race、Publication証拠を省略しない。

## Acceptance criteria

- 同じController-backed fixtureで `remote-ahead`、`overlapping-uncommitted`、primary divergence、in-flight lease / source不一致を再現し、`ProjectParentCompletion` と実際の `adopt-mutable` admissionの両方を照合する。実行側が拒否する既知状態で、handoffが無条件に実行可能な `adopt-mutable` を提示しないことを示す。
- 正当なremote/local一致のfast-forwardでは合法なtyped `adopt-mutable` が提示され、実行後にfresh attempt / Review / Validationへ復帰できる。
- source、remote、Controller generationを投影後に変更した場合も、実行時のfreshness guardが維持される。拒否時はController/remoteを変更せず、親が理由を把握できる。
- 実際の親handoff / staged CLI経路の回帰テストを含み、controller単体の返却値だけで正常復旧を主張しない。
- 修正前後の親tool/model往復・外部I/O・検証負担を測れる範囲で記録し、誤った拒否やGuard弱体化によるQuality Deltaを発生させない。
- 関連する正規テスト・lint、source-bound独立Reviewを実施する。未観測の実Dogfood再発防止を実測済みと称さない。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。監査Issue・PRの管理番号は実装要求の第二正本としない。
- 通常GLM Worker、Codex semantic判断、独立Review、immutable source evidence、1 Task 1 Commit、現ACTIVEとの境界を維持する。
- 同一不変条件を現行ownerが既に解決していると証明できた場合は重複実装せず、現在の検証証拠と結び付けてNo Changeと判断する。

## Dependencies

none
