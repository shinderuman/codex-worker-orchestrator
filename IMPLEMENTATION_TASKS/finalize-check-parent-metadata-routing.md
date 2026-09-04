# Task: Finalize-check routing across parent metadata changes

## Original instruction

````text
今後も作業中に改善要素を見つけたら随時タスクに積むように
````

## Amendments

none

## Resolved references

- install-smoke failure evidence taskのSol review中、PR 345 findingをparent-managed Plan/Taskへ追加した後に`glm-parent-action finalize-check go-test`をrepository rootから実行した
- implementation diffは不変だったがparent-managed metadataでexact snapshotが変わり、既存validationの`working_dir=/Users/shinderumanm/src/codex-config/glm-worker` routing evidenceが使われずcaller cwdへfallbackした
- validation `6ef64d137b30252a566ba301a252a330`は30msで`directory prefix . does not contain main module`失敗し、module rootからvalidation `b155f2352f65502b2507a2a5cf71fc13`を再実行して154224msでPASSした

## Purpose

parent-managed metadataだけの変更でfresh quality gateのmodule routingを失わず、誤cwdの即時失敗と親Codexの再判断・再実行を防ぐ。

## External feasibility

status: not-applicable

## Contract

- `finalize-check`はvalidation結果そのものをcache reuseせず、fresh gateの安全なworking directory routing evidenceだけを再利用できるようにする
- current HEAD/indexとimplementation worktree digestが既存current-task validationに一致し、差分がparent-managed metadata集合だけである場合は、そのvalidationの検証済みworking directoryをfresh gateへ使う
- implementation file、index、HEAD、task/session identityが変わった場合は旧routing evidenceを流用しない
- routing先がcurrent repository配下で、要求formを実行可能なmodule rootであることを機械確認する。確認不能時だけcaller cwd fallbackまたはstructured failureにする
- routing decisionと失敗理由をbounded machine resultへ残し、親Codexへcwd推測や同一gateの試行錯誤をさせない

## Must not

- completed validationのPASS/FAILをfresh gate結果として再利用しない
- parent-managed metadata guardを無視・緩和しない
- repository外working directoryや別checkoutへroutingしない
- module探索のためrepository-wide shell searchを親Codexへ要求しない

## Acceptance criteria

- parent-managed Plan/Taskだけを変更したsnapshotで、過去current-task validationのmodule rootへfresh gateが1回でroutingされる
- implementation diff、index、HEAD、task/sessionが変わるnegative caseでは旧working directoryを採用しない
- rootがGo moduleでないrepository layoutでも誤cwdの`go test ./...`を起動せず、選択根拠またはstructured failureを返す
- gateはfreshに実行され、validation_run_id・snapshot attribution・log locatorがcurrent stateへ一致する
- relevant test、independent reviewer、Sol review、current snapshot validation、commit/installを完了する

## Historical invariants

- quality coverageを減らさず、誤routingによる重複実行と親Codex turnだけを削減する

## Dependencies

none

## Review findings

none

## Current boundary

2026-09-04の実失敗2 validationを再現証拠として、現在task完了後のNEXTへ追加する。
