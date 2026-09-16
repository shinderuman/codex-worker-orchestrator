# Task: install smoke quality tools path resolution

## Original instruction

````text
いやそれならいままでなんでinstall smokeが通ってたの？
````

````text
そうじゃない、お前がまた通らない理由を捏造しているだけだと俺は思っている
````

````text
それは実行方法によって不具合になったりするのであれば直すべきなのではないのか、タスクに積まなくていいのか
````

## Amendments

none

## Resolved references

- 既定の `glm-parent-action install` では、install本体の完了後に `tests/install_claude_settings_location_smoke.sh` が一時HOME配下のcanonical quality-tools pathで `codex-worker-orchestrator-golangci-lint-2.7.0` を発見できず失敗した
- 同一source / installed状態で `QUALITY_TOOLS_BIN_DIR=/Users/shinderumanm/.local/share/codex-worker-orchestrator/quality-tools/bin` を明示した `glm-parent-action install` はinstall smokeを含め成功した
- 同じ種類の失敗は過去taskのinstall smokeでも発生しており、今回のproduction diff固有のregressionとは確定できない

## Purpose

install smokeのquality-tools解決が呼出時の環境変数指定有無で不安定になる状態を解消し、既定の正規install経路だけで再現可能かつ検証可能な結果を返す。

## External feasibility

status: not-applicable

## Contract

- `glm-parent-action install` から起動されるinstall smokeのquality-tools path解決経路と、一時HOMEを使うlocation smokeの責務境界を一次証拠から特定する
- 正規の既定install経路では、利用可能なcanonical quality toolsをcallerによる `QUALITY_TOOLS_BIN_DIR` の手動指定なしに一貫して解決する
- 明示的な `QUALITY_TOOLS_BIN_DIR` overrideは既存どおり尊重する
- required toolの欠落、version不一致、実行不能は成功扱いにせず、原因を判別可能なfailureとして維持する
- current taskの変更に由来しない既存環境差をproduction regressionとして誤分類しないよう、source / installed / tool pathの観測境界をtestで固定する

## Must not

- user固有の絶対pathをproduction実装またはtestへhard-codeしない
- missing tool検査をskipしたり、失敗を無条件に成功へ変換しない
- smoke成功のためだけに品質toolを無関係な一時HOMEへ恒久複製しない
- callerが毎回環境変数を手動設定する運用を恒久解決にしない
- 現在のACTIVE taskへ本変更を混在させない

## Acceptance criteria

- `QUALITY_TOOLS_BIN_DIR` を手動指定しない正規の `glm-parent-action install` が、canonical quality toolsを利用可能な環境でinstall smokeまで成功する
- 一時HOMEを使うlocation smokeが実際のinstall先検証とquality-tools discoveryを混同せず、必要なtool pathを決定的に受け渡す
- explicit override、既定path、required tool欠落、version不一致をそれぞれfixtureまたはintegration testで固定する
- error出力から解決を試みたpath、欠落tool、期待versionを判別できる
- install済みruntimeとsourceの一致確認、および既定呼出でのproduction smokeを通す

## Historical invariants

- runtimeへ影響するtaskはinstalled/source一致とinstalled状態での必要なproduction smokeを完了条件とする
- install smokeのfailureを根拠なくcurrent production diffのregressionへ帰属させない

## Dependencies

none
