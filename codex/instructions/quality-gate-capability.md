# capability必要quality gateの実行境界

親Codexが自身の検証・final gateを扱う場合だけ適用する。GLM worker/reviewerの検証はwrapper契約を正とする。

## machine-owned quality gate

このrepositoryのfull Go suiteはsandboxで成立しないUnix socket capabilityを含むため、直接`go test`へfallbackせずrepositoryの固定quality-gate surfaceを使う。

- full suite / race suiteの実行、固定argv・環境、実行directory、run identity、snapshot binding、同一running runへのattach、log/evidence保存、terminal resultはmachine側が所有する。親はこれらを再構成しない。
- finalizationでは`glm-parent-action finalize-check <go-test|go-test-race>`をcanonical入口とし、候補tree確定後（accept後、promote/publish/Task完了前）のpositionで必要formごとに1回だけ実行する。対象formと検証directoryはaccepted candidateの`required_validations`（form, working_dir）宣言に従ってroot・子Taskとも候補専用checkoutで実行し、宣言のないform・候補より導出できないdirectoryはfail closedに扱う。validation freshnessの整合は`control:quality-snapshot-binding`がgate記録自身のsnapshot bindingとpublication側のcompletion readiness検査へ委ねる。同じ目的で`--quality-gate`、status、handoff、snapshot比較を手作業で連結しない。
- machine resultがreadyなら、そのevidenceを使って最終semantic acceptanceを親Codexが判断する。blocked/failureならstage・reason・同梱evidenceを確認し、別directory・cache・commandを推測して成功へ読み替えない。
- tool sessionを失った既存runの回収だけはmachineが返したexact run identityのstatus/watch/result surfaceを使い、新規runとの二重実行や周期pollを行わない。

## Workflow接続検証

検証配置・最終source binding・Publication/Fix接続を変更するTaskでは、通常成功と最終gate失敗後の再入場をState補正なしで通す `go -C glm-worker test -tags=workflowintegration -run '^TestCanonicalValidationCadenceWithoutRuntimeRepair$' ./internal/app` を変更に対応する検証として実行する。通常full suiteへ重複して含めず、専用build tagで明示実行する。モデルとGo test/vet/buildはfixtureの代替を使う制御経路検証なので、実suiteの品質・所要時間の証拠にせず、必要な実full suiteは上記の正規finalize-checkで別に成立させる。

## sandbox内gate

`go vet` / `go build`相当はrepositoryの固定`goquality`入口、その他capabilityを必要としないrepository gateは既存のsandbox-safe入口を使う。Go version、cache path、managed quality tool pathを親Codexがtaskごとに合成しない。

診断用の`go test`(focused package・`-run`・`-race`等、正規finalize-check外の局所検証)も同じ`goquality test <go test args...>`入口を使う。入口が規定Go versionに解決できないときは検証として実行せず失敗として扱う。入口は正規gateと同じGo version固定とcache選択(ambient継承)を使い、`goquality-diagnostic`行(実go version・OS/arch・cache実値・head・source digest(内容束縛)・argv・exit/source変化)を診断証拠としてtask logへ残し、正規finalize-checkのvalidation証拠やmachine gateのPASSと混同しない。直接`go test`を規定外versionで実行した結果を検証成立として報告しない。

Mac上で共通のGoテストを局所検証する場合、repositoryが`goquality test-linux`を提供しローカルDocker ARM64を利用可能なら、その入口でLinux診断を実行できる。source転送・実Go/platform・cache・準備時間を記録し、Docker診断のPASSは正規validationへ昇格しない。macOS専用の検証はnativeの`goquality test`を使う。Docker未導入時は自動installせず、正規finalize-checkは引き続き既存の品質契約に従う。

GLM workflow内のmachine-fix / check-only順序、workerから直接実行を拒否するtool群、installer smoke等の既知procedureはproduction command/wrapperを正とし、本instructionへ複製しない。

## fail closed

- quality gate failureをsandbox由来と推測してskip・成功扱いしない。
- capability不足が一次証拠で確認された場合だけrepositoryの固定machine surfaceへ最小追加し、汎用command実行権限を広げない。
- 同一snapshot・同一目的のgateを環境選択やevidence再取得のためだけに重複実行しない。
- validationの意味的十分性と最終accept/fix判断は親Codexに残す。
