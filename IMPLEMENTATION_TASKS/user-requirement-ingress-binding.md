# Task: User requirement ingress binding

## Original instruction

````text
そもそも自由言語でハーネスにしようとしているのが間違いなんじゃねえのか
他に今までやった、これからやるものが自由言語で防ごうとしているものがあるんじゃねえのか
お前にはルールを守る能力がないんだから自由言語で防ぐことは不可能
````

## Amendments

none

## Resolved references

- `001-requirement-task-lifecycle.md`は新規ユーザー要求を次のGLM call前にtask fileへ固定する契約を導入した
- current harnessはACTIVE task fileの存在・構造・hashとworker/reviewerによる読取りを検証するが、直近ユーザーmessageのsemantic deltaがAmendmentまたは別taskへ反映されたかは認識できない
- 今回も改善Task化の追加指示がRulesに存在したまま、親Codexが外部拒否を復旧して次へ進もうとした

## Purpose

最新ユーザー要求を会話memoryだけに残したままGLM dispatch、長時間wait、completionへ進むことを、自由言語遵守ではなくmachine-visible ingress stateで防ぐ。

## External feasibility

status: observation
assumption: Codex appがrepository commandへuser turn identityまたは親がlosslessに渡せるbounded bindingを提供できる範囲で、次action admissionへ結び付けられる

## Contract

- user turnとtracked Amendment/新task/run-control dispositionを対応付けるbounded machine recordを設計する
- 次のmodel call/state-changing parent actionは未処理ingressがない場合だけ許可する
- semantic requirement、run-control、質問のみを親Solが分類し、その結果をhash/locator付きで記録する。分類自体をGLMへ追加委譲しない
- repository/tool境界からuser turn identityを取得不能なら、取得不能範囲を一次証拠で特定し、虚偽の機械保証を作らない
- 既存ACTIVE task本文とUSER_REQUESTの重複を増やさない

## Must not

- 親Codexが「反映した」と自由文で宣言するだけのgateにしない
- user message本文をtelemetryやpromptへ無制限複製しない
- generic LLM classifierや追加AI callを導入しない
- 質問・status確認までsemantic Amendmentとして保存しない

## Acceptance criteria

- 未処理semantic amendmentを残したdispatch/completionがfail closedするscenarioがある
- run-controlのみ、質問のみ、別task要求、ACTIVE amendmentを区別してbounded dispositionを保持する
- compaction後もuser turn bindingとtracked locatorを再検証できる
- 外部API制約で完全強制不能な部分は`external-unenforceable`として監査inventoryへ戻り、instruction追記だけで完了扱いしない
- 独立reviewer、Sol semantic review、current snapshot validationを完了する

## Historical invariants

- task requirementの正本はtracked task fileであり、会話要約ではない
- semantic分類の最終authorityは親Codexに残す

## Dependencies

- `IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`

## Review findings

none

## Current boundary

auditで実行可能なapp/command ingress surfaceを確定してから実装する。
