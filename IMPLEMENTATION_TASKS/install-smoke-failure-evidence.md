# Task: Install smoke failure evidence

## Original instruction

````text
さっきの「意味のある停止」というのが何なのか知らないが、そういうのを改善したほうがいいと思うなら随時タスクに積んでいくように
今後も作業中に改善要素を見つけたら随時タスクに積むように
022の前での再評価タスクでも改めてタスクに積むべきものがなかったか精査するように
````

## Amendments

none

## Resolved references

- `glm-worker --install-smoke --role parent`が同一snapshotでexit 1を返したが、`glm-worker/internal/app/install_smoke.go`が子scriptのstdout/stderrを`io.Discard`へ捨て、validation recordのlogも空だった
- 原因確認にはunderlying `tests/install_smoke.sh`と`sh -x`の追加実行が必要になり、最終的にmacOSの`/var/folders/...`と`/private/var/folders/...`のphysical-path差によるassertion failureと判明した
- current taskだけで親Codexの追加診断turnとGLM調査/review roundが発生し、Sol/Codex実消費削減の最上位目的に反する再発可能なobservability gapになった

## Purpose

install smoke失敗を追加実行なしで診断できるbounded evidenceを残し、親Codexの再探索・GLM再roundを削減する。

## External feasibility

status: not-applicable

## Contract

- `glm-worker --install-smoke`が失敗した場合、underlying scriptのstdout/stderrから原因特定に必要なbounded evidenceを既存task/session artifactへ保存する
- process errorにはvalidation recordと同じexact evidence locatorを含め、親Codexがrepository-wide探索や同一smoke再実行をせず参照できるようにする
- evidenceは成功時のmodel-visible outputへ混入させず、失敗時もraw全文をmachine JSONへinlineしない
- credential、token、cookie、session情報等を保存しない。保存前のsanitization、size上限、retentionは既存artifact lifecycleへ従う
- script exit code、wrapper起動失敗、evidence保存失敗を区別し、evidence保存失敗をsmoke成功へ誤変換しない
- current validation recordのsnapshot attributionとroleを維持する

## Must not

- install smokeを自動再実行して原因を推定しない
- 新しいdaemon、state DB、無期限log storageを追加しない
- 成功runのstdout/stderrを無条件保存しない
- secretをraw artifactまたはtelemetryへ保存しない
- install smokeのacceptance coverageを縮小しない

## Acceptance criteria

- 故意に失敗するfixtureで、1回の`--install-smoke`からexit source、exit code、bounded/sanitized evidence artifactとexact locatorを取得できる
- stdout/stderrの上限超過、binary/invalid UTF-8、evidence保存失敗を安全に扱うtestがある
- success pathの既存single-object stdoutとvalidation semanticsが不変である
- failure resultから同一scriptの再実行なしで失敗assertionを特定できるintegration testがある
- harnesslint、関連Go test、install smoke、独立review、current snapshot validationを完了する

## Historical invariants

- structure化machine errorを維持し、raw transcriptをSol-visible JSONへ展開しない
- GLM/Codexの品質coverageを落とさず反復実行だけを削減する

## Dependencies

none

## Review findings

none

## Current boundary

Codex command approval coverage taskで得たdiagnostic gapを独立責務として扱う。
