# Task: install smokeの重複品質証拠とloop反復costを削減

## Original instruction

````text
今回観測された長時間install smokeについて、局所的な改善と、
今後同種の問題を自律的に拾うためのloop engineering改善をまとめて実施してください。

現在ACTIVE taskの正常進行は妨げないこと。
単に処理が遅いという理由でtest・quality gateをskipしたり、acceptance criteriaを弱めたりしないこと。
必要なら現在task完了後の独立taskとして扱うこと。

## 1. 今回の具体的な改善対象

今回の実ログでは、`tests/install_smoke.sh` のfull runが約20分以上かかっている。

一次証拠上、

- install smokeには多数のinstall scenarioがある
- 各scenarioから`install.sh`を実行している
- `install.sh`のpreflightで`go test ./...`が走る
- `glm-worker/internal/workflow`だけでも1回約60秒かかる実行がある
- そのため同等のfull Go test suiteがinstall scenarioごとに多数回反復されている
- worker/reviewer/fix/re-reviewの各roundでもfull smoke自体が再実行されるため、wall-clock costが増幅している

ことが確認されている。

この構造を調査し、install smokeが保証しているsemantic coverageを失わずに、
不要な重複実行を削減できるか設計・実装すること。

特に以下を確認すること。

- 各install scenarioでreal `go test ./...`を実行する必要が本当にあるか
- installer自体のcontract検証と、glm-worker本体のfull Go test検証を分離できないか
- real full testが必要な代表scenarioと、go shim/mockで「正しくtestを起動しようとしたこと」だけ確認すれば十分なscenarioを分離できないか
- success/failure/preflight/override/plan-gate等のscenarioごとに、何をsemanticに保証したいのかを整理すること
- 同じfull suiteを繰り返さない形にしても、install.shが本番で必要なtestを実行するcontract自体は失わないこと
- test高速化のためにproduction install behaviorを弱めないこと
- timeout短縮、単なる並列化、assertion削除だけで解決扱いにしないこと
- flaky化や共有state競合を増やす並列化は避けること

改善前後について少なくとも、

- full install smoke wall-clock
- real `go test ./...`実行回数
- scenario数
- semantic coverageの対応関係
  を確認し、なぜcoverageを落としていないと言えるかを残すこと。

特定秒数を恒久contractへhardcodeしないこと。

## 2. 恒久的なloop engineering改善

今回のinstall smokeだけを直して終了せず、
今後の通常orchestrationで同種の反復コストを自律的に検出・改善候補化する規則を、
既存のparent orchestration product化判断ruleへ最小統合すること。

新しい巨大frameworkや別系統のoptimization subsystemは作らないこと。

対象は親Codexの手作業だけではなく、
worker / reviewer / test / build / lint / smoke / provider probe / polling /
resume verification等のmachine executionも含む。

### worker / reviewer側

現在taskを実行中に、
同一または実質同一の高コスト処理が複数回反復され、
task wall-clockの大部分を占めていることを一次証拠から確認した場合、

- 勝手にskip・縮退・最適化しない
- current taskのscopeを広げない
- task結果へ「反復コスト観測」として簡潔に報告する

こと。

報告には可能な範囲で、

- 何が反復しているか
- 何回程度か
- wall-clockの主要部分になっている根拠
- 正常処理なのかfailureなのか
- 改善余地の仮説
  を含める。

ただし同一候補を各review roundで繰り返し増殖させないこと。

### 親Codex側

通常orchestration中に反復コスト観測を受け取った、または自ら発見した場合、
ユーザーの個別指摘を待たず、別taskとして改善すべきか判断する。

判断基準は最低限以下。

1. 同一または意味的に重複した処理が反復しているか
2. 今回限りではなく今後の通常loopでも再発するか
3. 品質coverageを維持したまま実行回数・待ち時間・model/provider消費を減らせるか
4. expensive real executionとcheap contract/mock verificationを分離できるか
5. 改善実装と保守コストに見合うか
6. 改善によってfalse success、flakiness、観測不能化を生まないか

改善価値があると判断した場合は、
現在ACTIVE taskへ無関係なrefactorとして混ぜず、
semanticに独立したfollow-up taskとして通常Plan lifecycleへ追加すること。

一時的なmigration、一度限りの長時間処理、
意図的に必要なintegration test、
改善効果が小さいものはtask化しないこと。

## 3. 最上位目的

目的は単純な高速化ではない。

Sol High相当の品質を可能な限り維持したまま、

worker → test → reviewer → test → fix → reviewer ...

というfeedback loop全体に存在する
不要なwall-clock、Codex/Sol消費、GLM/provider消費を減らすこと。

したがって、
「遅い処理を削る」のではなく、
「同じ品質証拠を不必要に何度も取り直している箇所を減らす」
ことを基本方針とする。

今回のinstall smokeはこの恒久規則の最初の実例として扱ってよいが、
install smoke専用規則にしないこと。

既存のparent orchestration product化判断ruleで既に表現できる内容は重複追加せず、
そのruleをmachine executionの反復costまで含むように最小拡張すること。
````

## Amendments

none

## Resolved references

- 「今回の実ログ」はTask 015のreview/fix/re-reviewで`tests/install_smoke.sh`が複数回full実行され、各runが多数の`install.sh` preflightから同一Go suiteを反復した観測を指す
- 「既存のparent orchestration product化判断rule」は`IMPLEMENTATION_RULES.md`の`## parent orchestrationのproduct化判断`を指す

## External feasibility

status: not-applicable

## Purpose

同じ品質証拠をfeedback loop内で不必要に再取得する反復を減らし、install production contractとsemantic coverageを維持したままwall-clockとmodel/provider消費を削減する。

## Contract

- install scenarioごとのsemantic保証とreal Go suite実行の必要性を一次証拠で対応付け、代表的なexpensive real executionとcheap contract/mock verificationを分離する
- production `install.sh`が必要なtestを実行する契約を弱めず、install smoke harness側の重複だけを削減する
- 改善前後のfull smoke wall-clock、real `go test ./...`回数、scenario数、semantic coverage対応を再現可能に記録する
- 親product化判断をmachine executionの反復costへ最小拡張し、worker/reviewerはcurrent scopeを変えず一次証拠付き観測だけを重複なく報告する
- 親Codexは報告または自己観測から、再発性・coverage維持・real/cheap分離・費用対効果・false success/flakiness/観測不能riskを評価し、価値があれば独立taskを追加する

## Must not

- 遅さだけを理由にtest、quality gate、acceptance criteria、production install behaviorをskip・縮退しない
- timeout短縮、単純並列化、assertion削除だけで完了扱いにしない
- flaky化、共有state競合、false success、観測不能化を増やさない
- current Task 009へ実装を混ぜない
- 巨大framework、別optimization subsystem、install smoke専用の重複恒久ruleを作らない
- worker/reviewerが観測を理由にcurrent taskのscopeを自律拡張しない

## Acceptance criteria

- install scenario分類とsemantic coverage表が存在し、real full suiteが必要な代表caseと呼出contractだけで足りるcaseの境界を説明できる
- production `install.sh`のtest実行contractを維持したまま、full smokeでのreal `go test ./...`反復回数とwall-clockが減る
- scenario数と意味的assertionを不要に減らさず、success/failure/preflight/override/plan-gate等のcoverage非退行をtestで固定する
- before/afterのwall-clock、real suite実行回数、scenario数を測定し、特定秒数を恒久contractへhardcodeしない
- `IMPLEMENTATION_RULES.md`の既存product化判断をmachine execution反復costまで最小拡張し、worker/reviewerの重複しない観測報告contractをproduction promptと必要なtestへ同期する
- 一度限り・意図的integration・効果小の候補はtask化しない境界を維持する
- 関連test、全test/race/vet/build/gofmt、独立reviewer、risk/contractに応じたSol品質gate、親Codex commit/install

## Historical invariants

- 最上位目的はSol High相当品質を可能な限り維持しつつCodex/Sol実消費を大幅に削減すること
- `IMPLEMENTATION_RULES.md`の既存parent orchestration product化判断とtask粒度/dispatch contract
- production installerのpreflight fail-closed、final HEAD gate、Claude CLI verification、managed配置前検証

## Dependencies

none

## Review findings

none

## Current boundary

Task 009は完了。明示最優先のsource comment absolute invariant taskとcommit authorization source taskの後に独立taskとして開始する。Task 015で観測した20分超full smokeを最初の実例とするが、品質証拠の再取得削減という一般境界へ限定する。
