# Task: 親Codexのtask finalization choreographyを削減する

## Original instruction

````text
正直Codexの無駄はこれから全然削っていくところだと思っている
つまりCodexになにかタスクを与えて動かしてなにか無駄がないか確認するタスクが常に必要
今回のBundleを出させるタスクもその1つだ
````

## Amendments

````text
CommentlintとBundle Diff以外の実装をお前が全部終えてその次にCommentlintをやらせて観測するつもり
````

## Resolved references

- completed evidence-bundle dogfoodでは、長時間`glm-parent-action resume`がterminalへ戻った後の約9分だけでparent inputが約3.35M tokens増加し、約3.23Mがcached、37 tool calls中33がexecだった。
- 同区間にはsemantic diff review/acceptance判断もある一方、quality-gate run回収、bundle manifest確認、telemetry schema探索、Git pre/post-push確認等のdeterministic ceremonyが多数含まれた。
- semantic review、fix/accept判断、unexpected Git state判断は親Codex authorityとして残す。

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

none

## Review findings

none

## Current boundary

未着手。次のCodex+GLM dogfoodより先にWeb GPT側で完了する。
