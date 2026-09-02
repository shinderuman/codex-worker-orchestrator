# Task: CLI structure invariantsをmechanical guardへ昇格する

## Original instruction

````text
JSON化、Mainの薄いエントリポイントというのも機械的に縛りたい。
IMPLEMENTATION_RULESを確認し、そこにJSON化、Mainの薄いエントリポイントというのが書かれていなければ記載する。
````

## Purpose

`glm-worker`の新しいcommandやearly-return pathを追加した際に、reviewだけへ依存せず次の二つをrepository quality gateで機械的に守る。

- Goの`cmd/*/main.go`は起動処理だけを行う薄いentrypointとし、feature固有のCLI分岐・business logic・設定解析・永続化等を直接所有しない。
- `glm-worker`の成功stdoutは、streaming commandである`--watch`を除き単一のmachine-readable JSON objectとする。失敗時はstdoutを空に保ち、structured error JSONをstderrへ出す既存contractを維持する。

## External feasibility

status: not-applicable

## Contract

- current Gitと`IMPLEMENTATION_RULES.md`を最初に確認し、上記二つがcanonical ruleとして不足している場合は同fileへ明記する
- Go entrypoint enforcementは既存のRepository Lint / `harnesslint` ownerを拡張し、新しいstandalone lint frameworkを追加しない
- `cmd/*/main.go`はAST等の構造的判定で検査し、単なるLOC上限だけにはしない
- `main()`はinternal command/applicationへの起動委譲と、必要最小限のterminal error handlingだけを許容する
- `main()`内のfeature-specificな`os.Args`分岐、command dispatch、config load、state/persistence、HTTP handler等はfailさせる
- regression fixtureとして、`main()`内で`os.Args[1] == "--authority"`等を判定して別commandを直接dispatchする形をRepository Lintが拒否することを固定する
- 現在の正当なthin entrypoint群はpassさせる。複数binaryを無理に一つのapplication packageへ統合しない
- `glm-worker` machine-output enforcementは既存のsingle-object validator / process-level machine-output ownerを再利用し、第二のJSON contractや独立parserを追加しない
- normal non-streaming commandだけでなく、help/bootstrap/early-return pathも単一JSON object contractを通過する構造にする
- successful early commandがplain textやJSON + trailing text等を出した場合、実stdoutへreleaseする前に失敗する回帰testを追加する
- `--watch`は既存どおりJSON Lines streaming success surfaceとして維持する
- failure時のstdout empty / structured stderr JSON contractを維持する
- enforcement追加によるmodel call、runtime orchestration call、通常実行時の意味のあるoverheadを増やさない

## Must not

- mainの薄さを任意の行数だけで判定しない
- `main()`から通常の起動委譲やterminal error処理まで禁止しない
- generic architecture analyzerを作らない
- unrelated command binaryを一つのapplication ownerへ強制統合しない
- machine-output JSON semanticsを`harnesslint`側へ複製しない
- `--watch`のJSONL streamingを単一JSON objectへ変更しない
- human-readable parallel outputを追加しない
- GitHub Issue / PR / CI management情報をtask requirementとして扱わない
- GLMにcommit/pushさせない

## Acceptance criteria

- `IMPLEMENTATION_RULES.md`にthin Go entrypointと`glm-worker` machine JSON outputのdurable invariantが記載され、mechanical enforcement対象であることが分かる
- `main()`内にfeature-specific CLI branchを置く回帰fixtureがRepository Lintでfailする
- current `cmd/*/main.go`がthin entrypoint ruleをpassする
- successful non-streaming early/bootstrap/help pathのplain-text outputが既存machine-output contractでrejectされる
- successful normal command / help / bootstrapは単一JSON objectを維持する
- failureはstdout empty + structured stderr JSONを維持する
- `--watch`はJSON Lines exceptionとして維持する
- Repository Lintとfull Go suiteがpassする

## Historical invariants

- Goの`main.go`は起動処理だけを行う薄いentrypointとし、実装責務は`internal/`配下の適切なownerへ置く
- machine / LLM firstを維持し、同じ意味のhuman-readable presentation pathを追加しない
- 1 behavior / 1 authorityを維持し、machine-output parser/validatorを複数ownerへ分散させない

## Dependencies

- none

## Review findings

none

## Current boundary

Dogfood用の小規模implementation taskとして実行可能。既存Repository Lintとmachine-output ownerの狭い拡張に限定し、architecture redesignへ広げない。
