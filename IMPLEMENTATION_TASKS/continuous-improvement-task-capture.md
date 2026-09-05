# Task: Continuous improvement task capture

## Original instruction

````text
さっきの「意味のある停止」というのが何なのか知らないが、そういうのを改善したほうがいいと思うなら随時タスクに積んでいくように
今後も作業中に改善要素を見つけたら随時タスクに積むように
022の前での再評価タスクでも改めてタスクに積むべきものがなかったか精査するように
````

## Amendments

### 2026-09-04

````text
そっちの話じゃねえよ
この件に限らず改善タスクは積んでいけって言ってるだろ
````

````text
そもそも自由言語でハーネスにしようとしているのが間違いなんじゃねえのか
他に今までやった、これからやるものが自由言語で防ごうとしているものがあるんじゃねえのか
お前にはルールを守る能力がないんだから自由言語で防ぐことは不可能
````

````text
あと機械化したものは自由言語での制限は薄くしていいんじゃないのか
すべて消すと機械で制限されている根拠を見失う可能性があるので全部消すわけにはいかない
だが記載を薄くすることでお前は他の判断を重要しすることになるかもしれない
なぜなら文字数が多ければ多いほどお前はルールを見落とすからだ

あとこの「課題を見つけたら課題化する」というのも機械化可能なら機械化すべきじゃないのか
````

## Resolved references

- `IMPLEMENTATION_RULES.md`には改善候補を現在task完了まで待たず随時Task化する規則が既にあるが、2026-09-04のautomation安全判定拒否時に親Codexは復旧だけを行い、ユーザー指摘まで改善taskを追加しなかった
- 個別Heartbeat不具合は`IMPLEMENTATION_TASKS/auto-resume-heartbeat-transaction.md`で扱い、本taskは問題種別を問わず作業中の改善候補を捕捉・評価・Task化する親orchestration全体を扱う
- 105後の`post-105-codex-efficiency-reevaluation.md`は取りこぼしを再精査するsafety netであり、通常作業中の即時捕捉を代替しない
- 今回は既存Rulesに正しい自由言語規則があっても親Codexが適用しなかった。したがってRules・AGENTS・instruction・promptへの追記だけを再発防止harnessとして扱えない
- 本taskは改善候補捕捉だけでなく、過去に完了した対策とPlan上の予定taskについて、守るべきinvariantが自由言語だけに依存していないかを監査し、機械的precondition/postcondition/state transitionへ移す責務を含む
- 全体監査は独立責務として`IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`へ分割し、本taskはそのinventoryを継続実行時の候補捕捉へ接続する
- 現在のin-flight GLM sessionは再起動せず、作成済みHeartbeatによるresumeを継続する

## Purpose

作業中に観測した再発障害、不要なparent return、retry、手作業、token浪費、品質gap、観測欠損等を親Codexの記憶やユーザー再指摘へ依存せず、その意味のある状態遷移で評価し、実装価値がある候補を022より前のTaskへ確実に追加する。

## External feasibility

status: not-applicable

## Contract

- worker/reviewer/parent action、外部tool拒否、validation、resume、finalization等の意味のある状態遷移から、改善候補を低cardinalityの種類・一次証拠locator・想定Codex削減またはQuality Delta・再発性とともにboundedに提示できるproduction surfaceを設ける
- 親Codexが追加AI callやrepository-wide再探索を行わず、候補ごとに`taskized`、`既存taskへ統合`、`post-105再評価へ明示保留`、`非採用`のいずれかを理由付きで処理できるようにする
- 実装価値がある候補は現ACTIVEを中断せずparent-managed task fileとPlanへ即時固定し、現在task完了まで会話memoryや内部TODOだけに保持しない
- 同一原因・同一責務の候補はexisting current/history taskと機械照合するためのlocatorを返し、重複task作成とfalse-completeの見逃しを防ぐ
- 長時間wait、次のGLM dispatch、task completionの前に未処理のactionable候補が残っていないことをboundedに確認できるようにする。ただし無変化pollやliveness報告を増やさない
- 105後再評価では、非採用理由が弱い候補、明示保留、Task化されなかった観測を再列挙できるようにする
- `prose-only-control-enforcement-audit.md`のinventoryと後続taskを入力にし、今後の新しい候補にも同じ機械強制基準を適用する
- structured lifecycle/validation/tool failureから改善候補を自動生成し、未処理candidateをdurable stateへ保存する。親が思い出して自由文でTask化することを通常入口にしない
- 親Solがcandidateのsemantic dispositionとtask scopeだけを確定し、parent actionがexact candidate IDをconsumeして既存taskへの統合または新task/Plan追加の機械部分を実行する
- actionable candidateが未処理の間は長時間wait、次task開始、current task completionのadmissionで明示し、無視したまま遷移できないようにする

## Must not

- 改善候補のsemantic採否やtask scope決定をGLMへ最終委譲しない
- 候補評価のための追加model call、全ログ再読、raw prompt/review本文の複製を増やさない
- 単発の意図どおりの安全停止、通常のprovider limit、品質維持に寄与しない違和感を無条件にtask化しない
- 改善候補を現ACTIVEの実装scopeへ混在させない
- current taskを中断する必要がない候補を理由なく割り込みACTIVE化しない
- 105後再評価までTask化を先送りすることを通常経路にしない
- Rules、AGENTS、instruction、promptに「必ず」「禁止」「fail closed」と書くこと自体を機械的強制の代用にしない
- LLMが自由言語を常に遵守することをsafety propertyの前提にしない
- candidateをtelemetryへ記録するだけ、親promptへTask化を依頼するだけで機械化済みと扱わない

## Acceptance criteria

- 外部拒否、worker/reviewer finding、Sol差戻し、retry、validation重複、不要なparent return、観測欠損のfixtureで改善候補がboundedに提示される
- candidateはstable ID、cause/category、evidence locator、detected transition、disposition stateを持ち、同一原因の重複を機械抑止する
- exact candidate IDを処理しない限り後続admissionが未処理状態を返し、親の自由言語宣言だけでは解消できない
- taskized dispositionではtask fileとPlanのexactly-once閉包まで一つの親action postconditionとして検証される
- 候補の未処理、既存task重複、削除済みtaskのfalse-complete再発を検出できる
- actionable候補を現在taskを維持したままPlanへ追加し、その後同一sessionを継続するintegration scenarioがある
- 非採用・明示保留にはbounded reasonが必要で、105後再評価から再取得できる
- 新しいAI call、定期poll、liveness parent returnを増やさず、Codex-visible出力は判断に必要なfieldとexact locatorへ制限する
- 代表scenarioで自由言語指示を意図的に欠落・誤解しても、machine precondition/postconditionが違反操作またはfalse completionを拒否する
- 最終Acceptanceは「instructionを追加した」ではなく、実command/state/testで強制されることを根拠にする
- 独立reviewer、Sol semantic review、current snapshot validation、必要なinstall/smokeを完了する

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Deltaである
- GLM token削減のためにCodex / Sol実消費を増やさない
- 改善Task化は親Codexのsemantic authorityであり、GLMは候補と一次証拠を提示する

## Dependencies

- `IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`

## Review findings

- prose規則だけでは今回の捕捉漏れを防げず、ユーザー指摘が再び必要になった

## Current boundary

現ACTIVEと停止中の同一GLM sessionを変更しない。現ACTIVE完了後の最優先taskとして、随時Task化をproduction orchestrationへ組み込む。
