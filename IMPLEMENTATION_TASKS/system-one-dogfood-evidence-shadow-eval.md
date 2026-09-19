# Task: System-One Dogfood evidence shadow evaluation

## Original instruction

````text
https://typesafe.ai/blog/introducing-system-one-models-and-jev
https://note.com/npaka/n/n6f8dd30a5fa4

この思想って俺のツールになにか活かせる？

優先順高めで実装してみたい
Dogfoodで試したほうが良さそうだから
````

## Amendments

none

## Resolved references

- linked TypeSafe articleの中核は、自由文生成ではなく「unstructured state in, typed probabilistic decisions out」として、softwareが事前定義した質問・型・workflowの中で高速なprobabilistic decisionを返すSystem-One型modelを使うこと
- linked記事は、fine-grainedな独立判断、confidence/probability、smart if-statements、map-reduce型の大量data処理を主要use caseとして説明している。Jev固有実装へのlock-inではなく、このarchitectureを本taskの対象とする
- current orchestratorは既に deterministic machine enforcement と residual semantic parent judgment を分離しているため、その中間に「型は固定できるが意味判断が必要で、Sol Highの長い推論を毎回使うほどではない」bounded probabilistic decision layerをshadow導入する
- 最初の対象はDogfood bundle audit evidenceとし、既存Sol High監査をreferenceとしてclassifier/reducerのQuality DeltaとCodex/Sol token削減可能量を測る
- Jevのself-reported confidenceをそのままcorrectness証明にせず、Dogfood実績とのcalibrationを測る。Jevが利用不能・不適合なら同じtyped-decision contractを満たす代替modelでPoCできる
- deterministicに判定できるprovider code、task identity、Git snapshot、exact-once lifecycle等をprobabilistic modelへ移さない

## Purpose

Dogfood監査の大量evidenceに対し、typed probabilistic decisionを高速・低コストでshadow生成するSystem-One型layerを導入し、Sol Highへ渡すべき曖昧・高riskなtailだけを将来選別できるかを、現行監査結果を変えずに実測する。

## External feasibility

status: poc
assumption: Jevまたは同等のbounded structured-decision modelがDogfood evidenceの細粒度classificationを十分低コスト・低latencyかつcalibratableに処理でき、Quality Deltaを悪化させずSol-visible evidenceを有意に削減できること

## Contract

- 初期導入はshadow modeに限定し、System-One出力にcurrent Dogfood auditのcontrol authority、filter authority、Task/Issue disposition authorityを与えない
- canonical Dogfood auditは従来どおりfull evidenceをSol Highへ渡して完了させ、その最終finding/dispositionをshadow評価のreferenceとして保持する
- classifier inputはbundleのmachine-readable event/state/evidenceを優先し、自由文の巨大再要約を新たな入力authorityとして作らない
- outputは事前定義したtyped schemaで、少なくとも duplicate/noise likelihood、legitimate retry likelihood、correctness-risk、probable owner/disposition category、Sol escalation need、各decisionのprobability/confidenceを持つ
- classifier/reducerのschema外自由文をproduction decision sourceにしない。必要なdebug explanationはartifactへ分離し、routing authorityにしない
- Dogfood eventを独立またはbounded groupとしてmapし、同根・高confidence noise候補をreduceできるartifactを生成する
- shadow runごとにmodel/provider、input量、latency、cost/token相当、decision output、confidence、referenceとのagreement/disagreementを後から比較できるtelemetryを残す
- Quality Delta false negative、特にSol Highがcorrectness findingとしたevidenceをnoise扱いした割合を最重要riskとして測る
- confidence calibrationを測り、model自己申告confidenceだけでproduction thresholdを決めない
- coverageとして、将来Sol Highへ送らずに済む可能性があるevidence割合と、その場合の推定Sol/Codex実消費削減量を測る
- current deterministic machine-enforced invariantはSystem-One層へ移さず、semantic ambiguityが残るclassification/routingだけを対象にする
- PoC結果からproduction filtering/routingへ自動昇格しない。採用する場合はSol/parentがQuality Delta、calibration、coverage、token reduction、failure modeを見てGo/No-Goし、必要なproduction adoption責務をtracked化する

## Must not

- 初回DogfoodからSystem-One出力でevidenceをSol Highから不可逆に隠さない
- self-reported confidenceをmachine proofやground truthとして扱わない
- schemaが正しいこととsemantic decisionが正しいことを混同しない
- deterministic codeで完全に判定できるstate transitionやprotocol invariantをAI classifierへ移さない
- Jev固有APIへorchestrator全体を密結合させない
- token削減率だけを目的にcorrectness findingのrecallを下げない
- TypeSafeの公称speed/cost/accuracyをrepository内evalなしで採用根拠にしない

## Acceptance criteria

- 少なくとも1つのDogfood bundleをcurrent canonical auditと並行してshadow処理でき、canonical audit結果には影響しない
- typed schema validationがあり、schema外・欠損・provider failureはaudit本体を壊さずshadow failureとして観測できる
- Sol High final finding/dispositionとshadow decisionをeventまたはfinding単位で対応付けられるcomparison artifactがある
- correctness finding false negative、classification agreement、confidence calibration、coverage、latency/cost、推定Sol-visible input削減をbundle単位で計測できる
- high-confidence noise/redundancy候補について、どのthresholdならQuality Deltaを維持できそうかをhistorical/live Dogfood evidenceから評価できる
- PoC終了時に production routingへ進む / shadow継続 / 撤退 のGo/No-Go材料が揃い、結果が良くても自動的にはcontrol authorityを与えない
- related Go tests、repository lint、必要なDogfood fixture/evalがPASSする

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Deltaであり、速度やclassifier accuracy単独ではない
- machineで決定可能なものはmachine ownerへ、意味判断だけをmodelへ残す既存責務境界を維持する
- Sol Highは全件のmechanical noiseを読むmodelではなく、曖昧・高risk・高レバレッジなsemantic tailへ集中させる方向で削減を評価する

## Dependencies

none
