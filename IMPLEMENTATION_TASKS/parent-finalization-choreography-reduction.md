# Task: 親Codexのtask finalization choreographyを削減する

## Original instruction

````text
正直Codexの無駄はこれから全然削っていくところだと思っている
つまりCodexになにかタスクを与えて動かしてなにか無駄がないか確認するタスクが常に必要
今回のBundleを出させるタスクもその1つだ
````

## Amendments

````text
履歴が見つからなかった場合の話だがAcceptanceに限らず、どういう状態のファイルなのかわからないため全体的にそのまま作業するべきではないと思う
前段としてそのタスクのGo/No-Goを決めてから作業するべきだと思う
````

````text
CommentlintとBundle Diff以外の実装をお前が全部終えてその次にCommentlintをやらせて観測するつもり
````

## Resolved references

- completed evidence-bundle dogfoodでは、長時間`glm-parent-action resume`がterminalへ戻った後の約9分だけでparent inputが約3.35M tokens増加し、約3.23Mがcached、37 tool calls中33がexecだった。
- 同区間にはsemantic diff review/acceptance判断もある一方、quality-gate run回収、bundle manifest確認、telemetry schema探索、Git pre/post-push確認等のdeterministic ceremonyが多数含まれた。
- semantic review、fix/accept判断、unexpected Git state判断は親Codex authorityとして残す。

## External feasibility

status: not-applicable

## Purpose

terminal worker/reviewer結果から最終完了までの機械的な確認を少数のmachine-readable actionへ畳み、意味判断を隠さずparent model re-entry/token/tool choreographyを減らす。

## Contract

- real finalization traceをsemantic decisionとdeterministic mechanicsへ分類してから実装する。
- 既存authorityを再利用し、第二のtask state machineを作らない。
- semantic diff review、accept/fix、task completion判断、unexpected validation/Git stateは親に残す。
- deterministic status/evidence/validation/publish preconditionは、既存command outputの拡張または小さい専用actionでまとめる。
- machine resultは次のsemantic判断に必要な情報を1回で返し、`status -> grep -> sed -> status`の往復を減らす。
- divergence、validation failure、dirty/unexpected stateはfail back to parent judgmentする。

## Must not

- semantic reviewを自動acceptしない。
- unexpected Git divergenceを自動修復・force pushしない。
- commit messageを機械的にでっち上げてsemantic parent judgmentを消さない。
- generic RPC/daemon/frameworkを追加しない。

## Acceptance criteria

- representative completed taskでterminal worker/reviewer returnからfinal completionまでのparent tool/model round tripをmaterially削減できるproduction surfaceを追加する。
- current dogfood baselineの約3.35M input / 37 tool callsより、同等complexity時のfinalization costを下げる観測点を持つ。
- semantic review/fix/acceptとGit safety boundaryが維持される。
- next commentlint dogfood bundleからfinalization intervalを再計測できる。

## Historical invariants

- GLMはcommit/pushしない。
- parent Codex semantic authorityとrepository validationを維持する。
- force/non-fast-forwardは通常workflowへ含めない。

## Dependencies

- `IMPLEMENTATION_TASKS/unscheduled-task-state-reconciliation.md`

## Review findings

- 2026-08-31 commentlint dogfood: `glm-parent-action finalize-check go-test`はrepository rootだけでなく`glm-worker` module rootから呼んでもchild cwdをrepository rootへ固定し、`pattern ./...: directory prefix . does not contain main module or its selected dependencies`で24 ms以内に失敗した。run IDは`4482b558cd68123c591154f20bf3e4c4`、module rootからの再現は`d32b0aa4939bdd8bee83e0125b9c2789`。`parentactioncmd.execute`→`runFinalizationCheck(cfg.RepoRoot, ...)`→`runResolvedWorker`の`command.Dir = repoRoot`でcaller module cwdを失うことを現物確認した。既存固定quality gateは呼出cwdで実行する契約であり、同じcommentlint snapshotのmodule full test run `4b643bc596bbb01b02745da7e5991742`はPASSしている。
- 修正検討境界: repository identity/Git summary用rootとvalidation実行cwdを分離し、同一repo内の正当なmodule cwdを既存固定quality gateへ保持する。subdirectory moduleを持つ実repository fixtureでpublic CLIからchild cwdを検証し、任意command入口や無関係なgate変更は追加しない。今回のACTIVE commentlintには修正を混在させず、finalization cost削減のAcceptanceも未解決のまま保持する。

## Current boundary

production implementationはPR #175 `bdf0e4cfa531ee00f5b64076af16a4d9d1361b22`で統合済み。`glm-parent-action finalize-check <go-test|go-test-race>`が既存blocking quality gate、canonical handoff、current-snapshot validation照合、read-only local Git summaryを1 machine-readable actionへ集約する。semantic accept/fix、task完了判断、commit message、commit/fetch/push、divergence修復は親Codex authorityのまま維持し、通常fast-forward push成功後の同一成功証明だけを目的とする追加fetch/remote HEAD/post-push statusは行わない。残るAcceptanceは次のcommentlint dogfood bundleでterminal returnからfinal completionまでのparent input/tool round tripを約3.35M input / 37 tool calls baselineと比較すること。
