# ローカルLinux ARM64でのGo検証

正規検証は既存の`glm-parent-action finalize-check`を使う。`quality-tools.yml`でMacのDocker backendを宣言したrepositoryでは、Darwin ARM64の正規full/raceをローカルLinux ARM64で実行し、同じrunでDarwin固有のconfinement検証をnative実行する。Linux CIはnative実行を維持する。

正規Gateは転送したsource、実container toolchainとplatform、image・Git・cache・CPU/メモリ、準備・転送・実行・cleanup時間、Darwin補完の終了結果を記録する。補完の必須testがskip/欠落した場合もPASSにしない。Docker利用不能・source drift・転送不一致・cleanup失敗では明示的に失敗し、nativeへ自動fallbackしない。Go版は`quality-tools.yml`を正とする。

局所診断はrepository rootから次を使う。

```sh
./goquality test -json -timeout=45m -count=1 ./internal/app
./goquality test -json -timeout=45m -count=1 -run '^TestTypedParentCompletionWorkflowThroughProductionBoundary$' ./internal/app
```

対象指定を省くと`./...`を実行する。`quality-tools.yml`の`go-test-macos-backend: docker-linux-arm64`契約があるDarwin ARM64 hostでは、この入口はMac native実行の選択肢を持たずLinux ARM64 Docker診断へ機械的にroutingされる。backend判定は正規gateと同じGo側契約解決(`goquality --backend <repository>`が`qualitygate.DockerBackendActive`へ委譲)に従い、契約値の表記差・重複・unsupportedでは対象platformでnativeへ流れず失敗する。同じ契約が有効なGLM worker sessionでは`go test`直接実行(相対・絶対path・`-C`形式・versioned go実行形式を含む)をBash deny ruleとPATH上の`go` proxyが拒否し、sandboxは解決済みの選択endpointのUnix socket接続だけを例外として許可する。Docker利用不能時は入口が失敗となり、Mac nativeへの迂回や検証省略をしない。source転送とDocker実行ownerは正規Gateと共用するが、診断PASSは正規validationへ流用しない。`goquality-linux-diagnostic`とcontainer内の`goquality-diagnostic`行を実行証拠として保存する。

sourceは未コミット変更とindexを含むarchiveとしてcontainerのnative filesystemへ転送し、SHA-256を照合する。source構築のgit操作は退避先cloneだけで行い、保護対象worktreeでは読み取り操作しか使わないため、git authority proxy配下のGLM worker sessionでも同じ入口が成立する。Mac directoryのbind mountは使わない。標準initと非root userを使い、実行containerとhost一時sourceは終了時に削除する。toolchain imageとGo build cache volumeは再利用する。

Docker本体は自動installしない。local Unix socketのnative Linux ARM64 engineが必要。ignored inputがある作業領域は転送対象との相違を隠さず拒否する。正規Gateのcandidate checkoutは制御用staging等から分離する。submoduleとrepository外symlinkも明示的に拒否する。

直接PTY raw/READY経路は退役した。親payloadはtoken-bound stagingと内部のframed stdinで渡し、長さ/hash検証を維持する。Darwin補完は現役のOS固有処理を対象とし、退役したPTY経路を検証のために再導入しない。
