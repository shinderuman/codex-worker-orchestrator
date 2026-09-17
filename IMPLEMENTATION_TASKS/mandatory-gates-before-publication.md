# Task: mandatory gates before publication

## Original instruction

````text
git push origin b9ab4caa72e46bef256de92c9c8b9d173c82b14c:main --force-with-lease
git reset --soft HEAD^^
をした

お前のクソ行動を絶対再現させないためのタスクを `stale-handoff-active-task-attribution.md` のあとに置け
その後ですべてのゲートを確実にパスさせて通常フローに戻れ
````

## Amendments

none

## Resolved references

- 親Codexはfull Go test、goquality、独立review、Sol reviewの成功後に実装commit `2fceee4` を作成したが、その後のrequired install smokeが`runtime_install_smoke_failed`で失敗したにもかかわらず、完了metadata commit `035586e` とremote pushを続行した
- install結果は`status=install_verification_failed`、`required=true`、`reason=runtime_install_smoke_failed`を返し、後続`complete`も`runtime_install_evidence_missing`で未完了だった
- 親Codexはbinary配置成功、既知NEXT task由来のfailure、`completion_admitted=true`、`allowed_actions`をrequired Gate失敗の免除材料へ誤変換した。いずれもquality acceptanceを意味しない
- userはremote mainを`b9ab4ca`へforce-with-leaseで戻し、`git reset --soft HEAD^^`により不正に公開された2 commitをstaged candidateへ戻した
- current install runtimeはclean committed source treeを要求するため、branch commit前にinstall smokeを必須化するだけでは循環する。candidate snapshotの検証とbranch/remote publicationを分離する必要がある

## Purpose

required Gateが未実行・失敗・stale・snapshot不一致の状態で、親Codexが実装branch commit、完了metadata同期、remote push、task completeを進めることをmachine側で不可能にし、全Gate成功へbindingされたcandidateだけを通常flowで公開する。

## External feasibility

status: not-applicable

## Contract

- taskごとのrequired Gate集合をmachine authorityが確定し、親の自由文判断や`allowed_actions`から免除を推測させない
- source/test/review/finalize/install/installed-state smoke等のrequired Gateを、同一candidate snapshot identityへbindingする
- branchを進める実装commit、完了metadata commit、remote push、task completeは、required Gateが全件PASSかつcandidate snapshot一致の場合だけadmitする
- required GateのFAIL、未実行、evidence欠損、stale、snapshot mismatchはすべてfail closedとし、原因が既知NEXT task・環境差・別責務でも現在taskのGate免除にしない
- installがclean commitを要求する現行循環を解消し、branchを進めないcandidate commit / isolated worktree / immutable tree等のmachine-owned candidateからinstall・smokeを実行し、PASS後だけ同一内容をbranch commitへ昇格できるようにする
- candidate validationからbranch commitへの内容一致、install evidence、finalize evidence、remote OIDまでmachineが一つのpublication transactionとして照合する
- `allowed_actions`、`completion_admitted`、`stop_admitted`、binary配置成功、部分Gate成功を、全Gate成功の代用にしない
- parentがremote write前に単一のcanonical readiness projectionからGate名、status、run ID、snapshot binding、missing/failure reasonを確認できるようにする
- remote push前にpublication gateを強制し、通常の`git push`を手順上の注意だけでなくrepository-managed guardで拒否可能にする
- userが明示したrollback後のstaged candidateを保持し、Gate成功前に同じ不正commitを再作成・再pushしない

## Must not

- required Gate failureを既知不具合、別task責務、環境要因、部分install成功という理由で成功扱いしない
- Gate成功前にbranch commitして「後で検証」、remote pushして「後で修復」を通常flowにしない
- `--no-verify`、hook無効化、evidence file手動生成、status書換えでadmissionを迂回しない
- 親promptやAGENTSの注意書きだけを唯一の防止策にしない
- candidateと異なるsourceをcommit、install、push、completeしない
- 全Gateを毎回無条件再実行してCodex / provider消費を増やさず、snapshot一致するfresh evidenceは再利用する

## Acceptance criteria

- required install smokeがFAILした再現scenarioで、実装branch commit、完了metadata commit、remote push、task completeがすべてmachine拒否される
- binary配置成功、既知NEXT task由来failure、`completion_admitted=true`、`allowed_actions=[complete,install]`の各条件があってもGate FAILをoverrideできない
- branch未更新のcandidate snapshotに対してfull quality Gate、install、installed/source一致、production smokeを実行し、全PASS後だけ同一contentのcommitとpushを許可するintegration scenarioを固定する
- candidate validation後のsource/index/worktree変化、Gate evidence欠損、stale run、別HEAD、別tree digest、remote OID不一致をそれぞれfail closedする
- publication readiness projectionがrequired Gate全件とexact run/snapshot bindingを返し、親が追加探索せず採否判断できる
- `glm-parent-action complete`がinstall evidenceより先にremote syncを要求せず、Gate不足時は不足Gateを最初のblockerとして返す
- normal success flowで不要な重複full Gateを増やさず、Codex ReductionとQuality Deltaを計測する
- userのrollback後staged candidateについて、全Gate成功前に`2fceee4` / `035586e`相当をmainへ再commit・再pushしない

## Historical invariants

- Gateはrequired failureを停止させるために存在し、別task化や既知原因は免除条件ではない
- machine action admissionはtransport authorityであり、semantic quality acceptanceではない
- runtime影響taskはinstalled/source一致とinstalled状態のproduction smoke成功まで完了しない
- remote publicationはcurrent candidateへbindingされた全required Gate成功後だけ行う

## Dependencies

none
