# Forward-only compatibility regression

## Original instruction

> それを消すタスクをさっき同様に作ってくれ

## Amendments

none

## Resolved references

- 「それ」は、2026-09-21のcurrent `main` 監査で確認した後方互換性維持の再侵入を指す。
- `scripts/manage-pull-hook.sh` は旧installer ownership state (`version=1 ... .githooks`) を識別し、current managed snapshot stateへmigration/recoveryする経路を持つ。さらにpreexisting tracked `.githooks` をinstaller ownershipへadoptし、retire時に旧baselineへ復元する互換経路がある。`tests/install_hook_ownership_smoke.sh` がこれらの成功を回帰契約として固定している。
- `glm-worker/internal/taskcontract/dependencies.go` の `ParseReviewFindings` は、current Rulesが「findingなしはsection欠落」をcanonicalとした後も旧 `## Review findings\n\nnone` を `None=true` として受理し、repository completion判定がその結果を使用している。
- `glm-worker/internal/harnesslint/forward_only.go` の `forward-only-compatibility` ruleはGo側の旧schema migration/promotion等を検出する一方、shell側ではPATH binary promotion中心の検査しかなく、上記hook ownership migrationを検出できなかった。
- 既存forward-only方針は、unsupported old machine stateをcurrentへmigration/promotion/alias/fallbackせず、用途に応じreject / skip / reset / rebuild / delete / non-resumableとするもの。既存ユーザー所有dataの破壊防止は後方互換性とは分離する。

## Purpose

既存のforward-only repository contractに反して再導入されたcompatibility bridgeを削除し、同じ直接的な再侵入をshared repository gateで機械的に捕捉できる状態へ戻す。

## Contract

- hook ownership stateはcurrent schemaだけを正規入力とする。旧 `version=1` ownership/pending state、旧layout由来のmigration-pending state等をcurrent ownership stateへ自動変換・昇格・推定しない。
- 旧installer layoutまたはpreexisting `.githooks` の存在だけからcurrent installer ownershipを推定・claimしない。externally/user-owned `core.hooksPath` は破壊・上書きせず、current ownership provenanceがない場合はfail-closedまたはunchangedとして扱う。
- fresh current install、current managed snapshot refresh、current transaction recoveryは維持する。forward-only化を理由にcurrent-state recoveryを壊さない。
- Task Markdownのfindingなしcanonical representationは `Review findings` section欠落だけとする。旧 `Review findings: none` をcompletion上の「findingなし」として受理しない。
- `forward-only-compatibility` shared gateを、今回escapeしたshellの明示的なold-state migration/adoption/acceptance classまで拡張する。deterministicに識別できる直接shapeだけを対象とし、generic keyword banや任意shell dataflow解析へ広げない。
- production codeとtestの双方から、旧形式を正常成功させるためだけのcompatibility behavior/acceptance regressionを除去する。

## Must not

- legacy migration/promotionをneutralな名前へ変更して残さない。
- unsupported old stateをcurrent stateとして書き直すrepair pathを追加しない。
- user-owned/external hook設定やfileを削除・上書きすることでforward-onlyを達成しない。
- current hook publication protection、managed snapshot integrity、atomic refresh/rollback、external ownership fail-closed性を弱めない。
- algorithmic fallback、safe recovery、historical label等、old schema/input compatibilityではない正当な `fallback` / `legacy` 用法を一括禁止しない。
- `forward-only-compatibility` と並行する別validator/CI-only ruleを新設しない。

## Acceptance criteria

- `scripts/manage-pull-hook.sh` に旧 `version=1` ownership/pending stateをcurrent stateへmigrationするnormal pathが残っていない。
- interrupted legacy migration専用state/recoveryや、旧installer layoutをcurrent ownershipへ昇格するcompatibility-only state machineが残っていない。
- preexisting/external `core.hooksPath` はcurrent ownership provenanceなしにinstaller-ownedへclaimされず、ユーザー所有設定が保持されるfocused regressionがある。
- stateなし・external hooksPathなしのfresh install、current managed stateのrefresh、current transaction interruption recoveryは引き続き成立する。
- `ParseReviewFindings` / repository completion contractはsection欠落をfindingなしとして扱い、`## Review findings\n\nnone` はcanonical inputとして受理しないfocused regressionを持つ。
- shared `forward-only-compatibility` gateが、少なくとも今回のshell old-state -> current-state migration/adoption shapeと、旧形式acceptanceを保証するtest surfaceの再導入を検出する。
- gateはold stateをreject/skip/reset/delete/non-resumableとするforward-only処理、ordinary safety fallback、current-only install/recoveryを誤検出しないfocused allow regressionを持つ。
- current treeの対象classを再監査し、同じroot causeの明示的なcompatibility bridgeが残っていないことを確認する。
- repository標準validation（repository lint、vet、build、full Go test、installer smokeを含む）がPASSする。

## Historical invariants

- repositoryは後方互換性を持たせない。machine-only state/schemaはcurrent version/schemaだけを正規入力とし、old version/schemaのmigration・promotion・alias・推定fallbackを既定要件にしない。
- 旧stateを保持しないことと、ユーザー所有dataを破壊しないことは別の安全要件として両立させる。
- 既存shared ownerは `glm-worker/internal/harnesslint` の `forward-only-compatibility` ruleであり、新しい並行gateを作らない。

## Dependencies

none
