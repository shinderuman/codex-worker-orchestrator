# Task: terminal result budgeted projection

## Original instruction

````text
> 出力がtransport上で途中省略されたため、同じ結果を再実行せずcanonical recovery projectionから不足項目だけ回収します。



それいつもやってるけどそれもタスク化、改善しないと余計なCodexトークン消費するんじゃないの？このプロジェクトの最上位目的わかってるよな？？？
````

## Amendments

none

## Resolved references

- 現ACTIVEの`glm-parent-action resume`はterminal envelopeを1回で返したが、親tool callの`max_output_tokens=1200`に対して結果が約1893 tokensとなり、`decision`、`evidence`、`options`、`recommendation`、handoffの中間部分が省略された
- 親Codexは同じmodel処理を再実行しなかったが、`glm-worker --handoff recovery`、`glm-parent-evidence.md`再読、artifact locator確認などの追加turnを必要とした。recovery projectionはlifecycleとaction specだけで、失われたsemantic decision本文を補完しなかった
- `glm-execution.md`の長時間call例は外側`max_output_tokens=1000`を指定する一方、`parent_action_terminal`はterminal semantic resultとhandoffを同一envelopeで返すため、通常の`NEEDS_SOL_DECISION` / `NEEDS_SOL_REVIEW`でも上限超過が再発し得る
- 最上位目的はSol High相当の品質を維持しながらCodex / Sol側の実消費量を大幅に削減することであり、通常loopの追加recovery・再読・判断再構成はこの目的に直接反する

## Purpose

GLM terminal結果を親Codexのmodel-visible budget内へ決定的にprojectionし、通常のdecision / review / accept loopで出力省略と追加recovery turnを発生させず、必要な意味判断情報とcanonical次actionを1回で受け取れるようにする。

## External feasibility

status: not-applicable

## Contract

- 主`glm-parent-action` callが、設定された親model-visible output budget内でterminal semantic resultとcanonical handoffの判断必須fieldを1回で返す
- `NEEDS_SOL_DECISION`ではstatus、decision、options、recommendation、test obligations、必要なexact source locator、risk、handoff action specsを欠落させない
- `NEEDS_SOL_REVIEW`ではstatus、sol question、targets、risk、summary、必要なexact source locator、handoff action specsを欠落させない
- PASS / error / interrupted等も、親がsemantic dispositionと合法な次actionを判断する最小fieldを同じbounded projectionで返す
- 長いraw evidence、重複summary、既知のbaseline path、再取得可能な詳細はmodel-visible stdoutへ展開せず、artifact locatorとcount / ID / exact regionへ機械projectionする
- projectionで必須fieldをbudget内へ収められない場合は、成功結果の途中切断にせずstructuredなoverflow errorまたは明示されたbounded continuation surfaceを返す
- 通常の成功terminalで`glm-worker --handoff recovery`、広いartifact探索、instruction再読を追加しなくても次actionへ進める
- output budget、projection、dedup、overflow発生をtelemetryで計測し、Codex Reductionへの効果を検証可能にする

## Must not

- tool側の`max_output_tokens`を無制限に増やすだけで解決しない
- terminal envelope全体をrawのまま返してtransport truncationへ依存しない
- 欠落fieldを親Codexの推測、Git再探索、worker再実行で補わせない
- semantic decision本文とcanonical handoffを別々の通常pollへ分割し、常時2 callを必要にしない
- evidence削減によりSol判断に必要なoption、risk、test obligation、exact locatorを落とさない
- Desktop表示上の切断とrepository側projectionの責務を混同しない

## Acceptance criteria

- 現在再現した約1893-token相当以上の`NEEDS_SOL_DECISION` terminalを、productionで使用する親output budget内の単一結果として必須field欠損なく返すscenarioを固定する
- `NEEDS_SOL_REVIEW`、PASS、worker error、interruptedの代表terminalでも同じbudget contractを固定する
- oversized evidenceを含むpacketでraw本文を出さず、count / ID / exact locatorへprojectionされることを固定する
- budget超過時にJSON途中切断や一見成功する欠落projectionにならず、structured fail-closedまたはbounded continuationになることを固定する
- 正常terminal後の追加handoff recovery / artifact探索が不要であることをintegration testで固定する
- before / afterで親tool call数、model-visible bytesまたはtokens、追加recovery回数を比較し、品質必須fieldの維持とCodex消費削減を確認する

## Historical invariants

- terminalはsemantic resultとcanonical next-action authorityを同じ親復帰境界で提供する
- structured evidenceは最初のtool call内で判断必須fieldとexact locatorへprojectionする
- 欠損をunknown/errorとして表明し、raw全文fallbackや親の推測へ縮退しない

## Dependencies

none
