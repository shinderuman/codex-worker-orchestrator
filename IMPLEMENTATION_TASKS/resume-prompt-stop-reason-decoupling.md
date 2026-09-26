# Task: resume promptからtransport停止理由を分離する

## Original instruction

```text
そもそも5時間以外のときはどういう指示が飛んでいるの
わざわざ「5時間で止まった」とか伝える必要があるのか
復旧前と同じ指示内容を飛ばすんじゃダメなのか、ということが気になった
```

```text
IMPLEMENTATION_PLANに積んでおいてくれ
優先度はお前が判断して順序を決めていいが、いま問題が大きくでてないのでだいぶ優先度は低めだと思う
```

## Amendments

none

## Resolved references

- 現行 `glm-worker/internal/workflow/prompts.go` の `resumePrompt` は、`provider-unavailable` と `guard-recoverable` を除く経路を既定で `plan-limit` として説明するため、`interrupted` 等でも「Z.ai GLM Coding Planの5時間利用上限で中断しました」というmodel-visible文言になり得る。
- 現行machine stateは `ResumeStopKind` として `rate-limited` / `provider-unavailable` / `interrupted` / `guard-recoverable` / `quality-gate-recoverable` を区別し、rate-limit reset metadata等もcheckpoint側で保持している。
- ユーザーの「復旧前と同じ指示内容」は、保存済み `OriginalPrompt` をsemantic authorityとして維持し、通常resumeでは停止原因そのものではなく「同じtaskの未完了部分だけを続行する」ための最小resume contextだけを加える方向を指す。
- `guard-recoverable` はsession invalidation / rebuilt resume等で通常resumeと意味が異なるため、必要なspecialized recovery contextを維持する候補とする。
- `quality-gate-recoverable` でcompleted resultを再利用してmodel callを不要にできる既存経路は維持する。

## Purpose

quota・provider・user interruption等のtransport / lifecycle停止理由を不必要にGLM promptへ漏らさず、machine-owned stop semanticsとmodel-visible作業指示を分離する。通常resumeでは保存済みtask指示と未完了継続に必要な最小contextだけを渡し、誤ったstop reason表示と不要なorchestration detailを除く。

## External feasibility

status: not-applicable

## Contract

- `rate-limited` / `provider-unavailable` / `interrupted` の通常resumeは、stop reason固有の自然言語をmodelへ説明せず、共通のgeneric resume promptを使用する。
- generic resume promptは、保存済みcheckpoint / session context / working treeを尊重し、完了済み作業をやり直さず未完了部分だけを続行することを明示する。
- 保存済み `OriginalPrompt` を要求authorityとして維持し、resume時に元要求を再要約・再解釈・再構成しない。
- stop kind、reset boundary、provider classification、resume admission等のdeterministic lifecycle情報はmachine state / gate側に保持し、model promptから削ったことを理由にmachine validationを弱めない。
- `guard-recoverable` のようにresume方式自体が通常継続と異なる経路は、必要なspecialized recovery promptを維持する。必要性は「停止理由を説明したいから」ではなく、modelが作業継続方法を変える必要があるかで判断する。
- `quality-gate-recoverable` のcompleted result再利用等、modelを再呼出しせず復旧できる既存経路を維持する。
- `ResumeStopKind` の分岐でunknown / unsupported kindをrate-limit扱いへfallbackしない。既知kindを明示的に扱い、矛盾はfail closedする。
- result-correction、review resume、auto-fix resume等の既存stage semanticsと、元promptのauthority-preservationを維持する。
- relevant unit / workflow testsで各stop kindのmodel-visible promptとmachine state preservationを検証する。

## Must not

- `5時間利用上限` を別の曖昧なquota説明へ言い換えるだけで終わらせない。
- rate-limit reset時刻、provider state、interrupt retention、resume checkpoint等のmachine-owned情報を削除しない。
- 通常resumeをfresh taskとして再起動したり、元要求を新規promptとして再構成したりしない。
- `guard-recoverable` 等、resume方法そのものが異なる経路まで理由なくgeneric化しない。
- unknown stop kindを任意の既知reasonへfallbackしない。
- historical compatibilityのために旧reason-specific promptをproduction fallbackとして残さない。

## Acceptance criteria

- `rate-limited` / `provider-unavailable` / `interrupted` が同じgeneric continuation semanticsをmodelへ提示し、quota/provider固有の停止説明をmodel-visible promptへ含めない。
- generic resumeでも保存済み `OriginalPrompt` とtask/session/working-tree continuation contractが保持され、完了済み作業の再実行を促さない。
- `interrupted` が誤って5h plan-limitとして説明される現行default behaviorが消えている。
- `guard-recoverable` 等のspecialized recovery semanticsとquality-gate result reuseがregressionしていない。
- stop reason / reset boundary / provider metadata / resume admissionのmachine-owned検証が従来どおり機能する。
- unknown / inconsistent stop kindは誤分類せずfail closedする。
- relevant Go tests、repository lint、whole diff reviewがPASSする。

## Historical invariants

- deterministic lifecycle・quota・provider状態はmachine ownerへ寄せ、modelにはsemantic作業に必要な情報だけを渡す。
- resumeは同一taskの保存済みstate継続であり、新規task要求の再生成ではない。
- correctnessを維持したままCodex / GLMの不要なmodel-visible orchestration detailと再処理を減らす。

## Dependencies

none
