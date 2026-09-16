# capability必要quality gateの実行境界

親Codexが自身の検証・final gateを扱う場合だけ適用する。GLM worker/reviewerの検証はwrapper契約を正とする。

## machine-owned quality gate

このrepositoryのfull Go suiteはsandboxで成立しないUnix socket capabilityを含むため、直接`go test`へfallbackせずrepositoryの固定quality-gate surfaceを使う。

- full suite / race suiteの実行、固定argv・環境、実行directory、run identity、snapshot binding、同一running runへのattach、log/evidence保存、terminal resultはmachine側が所有する。親はこれらを再構成しない。
- finalizationでは`glm-parent-action finalize-check <go-test|go-test-race>`をcanonical入口とし、current handoff / repository snapshot / validation freshnessの整合は`control:quality-snapshot-binding`へ委ねる。同じ目的で`--quality-gate`、status、handoff、snapshot比較を手作業で連結しない。
- machine resultがreadyなら、そのevidenceを使って最終semantic acceptanceを親Codexが判断する。blocked/failureならstage・reason・同梱evidenceを確認し、別directory・cache・commandを推測して成功へ読み替えない。
- tool sessionを失った既存runの回収だけはmachineが返したexact run identityのstatus/watch/result surfaceを使い、新規runとの二重実行や周期pollを行わない。

## sandbox内gate

`go vet` / `go build`相当はrepositoryの固定`goquality`入口、その他capabilityを必要としないrepository gateは既存のsandbox-safe入口を使う。Go version、cache path、managed quality tool pathを親Codexがtaskごとに合成しない。

GLM workflow内のmachine-fix / check-only順序、workerから直接実行を拒否するtool群、installer smoke等の既知procedureはproduction command/wrapperを正とし、本instructionへ複製しない。

## fail closed

- quality gate failureをsandbox由来と推測してskip・成功扱いしない。
- capability不足が一次証拠で確認された場合だけrepositoryの固定machine surfaceへ最小追加し、汎用command実行権限を広げない。
- 同一snapshot・同一目的のgateを環境選択やevidence再取得のためだけに重複実行しない。
- validationの意味的十分性と最終accept/fix判断は親Codexに残す。
