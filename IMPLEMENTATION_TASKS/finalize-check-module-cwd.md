# Task: finalize-checkでvalidation module cwdを保持する

## Original instruction

````text
Codexの節約、Codexの判断、GLMの判断、無駄な行動等多角的な問題をひたすら洗え
````

## Amendments

````text
実装を開始する前に、起票したIssueにCodexにやらせられるタスクがあったらCodexにまわしてくれ
````

## Resolved references

- `glm-parent-action finalize-check go-test`はrepository rootにGo moduleがないcurrent repositoryでvalidationを開始できない。
- callerを`glm-worker` module rootへ移してもparent action wrapperがchild `command.Dir`をrepository rootへ固定するため同じfailureを再現する。
- real run `4482b558cd68123c591154f20bf3e4c4`とmodule-root再現`d32b0aa4939bdd8bee83e0125b9c2789`が根拠。
- 同一snapshotの既存direct quality gateはmodule cwdからPASSしている。
- repository identity/Git summaryのrootとquality validationのexecution cwdは責務が異なる。

## External feasibility

status: not-applicable

## Purpose

`finalize-check`がrepository identityを維持したまま、同一repository内の正当なcaller/module cwdで既存fixed quality gateを実行できるようにする。

## Contract

- `go-test|go-test-race`のfixed validation formだけを維持し、arbitrary command入口を追加しない。
- Git summary、task state、repository identityはrepository rootを正とする。
- validation execution cwdはcaller cwdを基準に、同一repository配下であることを機械確認して既存quality-gate subprocessへ保持する。
- repository外、解決不能、symlink等で既存authorityを越えるcwdはfail closedにする。
- direct `glm-worker --quality-gate`のworking-directory semanticsを変更しない。
- root module repositoryでは既存behaviorを維持する。

## Must not

- cwd問題を解決するためrepository rootへ一時go.mod/workspaceを作らない。
- shell command parserや任意cwd指定CLIを追加しない。
- Git summary rootをmodule cwdへ縮めない。
- validation failureをsuccessへ読み替えない。

## Acceptance criteria

- subdirectory Go moduleを持つrealistic repository fixtureでpublic `glm-parent-action finalize-check go-test` / `go-test-race`がcaller module cwdを保持し、direct quality gateと同じ成立性を示す。
- returned Git summaryはrepository rootのHEAD/statusを表す。
- repository外cwdは新しいexecution authorityを得ずfail closedする。
- root-module fixtureのexisting behaviorを維持する。
- full validationと独立reviewを通す。

## Historical invariants

- parent actionはsemantic acceptanceを行わない。
- quality gate formはfixed binary boundaryを維持する。
- Git publication authorityを変更しない。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。finalization measurement/Acceptance自体はrepository Planで管理せず、実装後のdogfood evidenceを外部監査で評価する。