# Task: Long-running task progress observability

## Original instruction

````text
これどうにかして完成率とか見られるとよいなあ
5hLimitを2ターン3ターン繰り返すと15時間とかになるからその状態が30%なのか70%なのかわかるといいんだが
とは言えその判断にあんまトークンは使いたくない
ソースの数だけ測っても意味がないし
ちなみにトークンの消費量でGLMが仕事しているのはわかるので「仕事しているのが分かればいい」わけではない
--watch のオプションが流れてれば何かしてるのは一応わかるしな
````

## Purpose

長時間のGLM task、特に5h limitを複数回跨ぐtaskについて、単なるlivenessではなく「全体のどの辺まで進んでいるか」を、追加LLM callや大きなtoken消費を発生させずmachine stateから粗く観測できるようにする。

## Contract

- `--watch` 等の既存observability surfaceへ、長時間taskのprogressを追加LLM callなしで投影する
- source数、diff行数、token消費量だけを完成度のauthorityにしない
- execution milestoneを持つtaskでは、completed/current/pending milestoneとcurrent lifecycle phaseを主要なmachine evidenceとして使う
- milestone数だけを単純除算した一点percentageを精密な完成率として表示しない
- milestoneごとの重さが不均一であることを前提に、粗いprogress band/rangeまたは同等の不確実性を明示した表現を使う
- current milestone内のphase寄与は固定ruleまたは過去telemetryから機械計算し、進捗表示のためだけにmodelへ推論させない
- execution_unit=single等、machine evidenceだけでは完成度を意味的に推定できないtaskでは、根拠の薄い数値を捏造せずindeterminateを許容する
- 既存GLM responseへprogress自己申告を追加する案は、machine-only projectionで不足が実証されるまで必須要件にしない
- 5h resume回数、elapsed time、current phase等を補助情報として表示してよいが、それ自体を完成率へ直接変換しない
- 将来Dogfood telemetryが十分蓄積した場合、phase/milestone別の実所要時間分布でprogress bandを校正できる設計にする

## Must not

- progress表示のためだけに追加のGLM / Sol / Codex model callを発生させない
- token消費量を「仕事量」や完成率のproxyとして扱わない
- source/file数やdiff sizeを主たる完成度指標にしない
- 30%や70%のような一点数値を、machine evidenceが支えないのに確定値として表示しない
- liveness表示だけを本taskの達成とみなさない

## Acceptance criteria

- milestone taskについて、completed/current/pending milestoneとcurrent phaseから追加model callなしでprogress band/rangeを算出できる
- `--watch` またはcanonical observability surfaceで、少なくとも progress、current milestone、current phase、5h resume count、elapsed timeを確認できる
- milestone weightingが不均一でも誤って精密な一点percentageを保証しない
- single-unit taskなど推定不能な場合は`indeterminate`または同等の明示状態を返せる
- progress projection自体の計算はdeterministicでtest可能である
- 既存watch/liveness behaviorを壊さない
- related Go tests、repository lint、必要なfixtureがPASSする

## Historical invariants

- 目的は「動いていること」の確認ではなく、長時間taskが序盤・中盤・終盤のどこにいるかを低コストで判断できること
- 5h self-resumeを反復するtaskではwall-clockが長くなり得るため、elapsed time単独では残作業量を推定しない
- 最上位EvalはCodex ReductionとQuality Deltaであり、progress observabilityが追加model tokenを常態化させてはならない

## Dependencies

- 5h self-resume taskと独立に実装可能だが、同task導入後はresume count等のobservabilityを統合する
