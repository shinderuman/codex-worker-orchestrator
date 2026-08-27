# Task: 必要ruleをchange/operationから決定論的に適用する

## Original instruction

````text
F5: 規則を読む必要があるかをGLM自身が判断している

- 「テストが関係する場合はtesting.md」
- 「永続状態に関わる場合はstate-transitions.md」
- 「該当言語/CLI規則だけ読む」
等、規則適用判定者が規則で拘束される本人になっている。
- GLMが変更の意味を狭く解釈すると必要ruleを読まずに実装できる。

REQUIRED HARDENING

- rule適用条件を可能な範囲でpath/change-type/operationからdeterministicに導出する。
- persistent state/config/cache/migration対象変更 → state-transition gate
- CLI surface変更 → CLI contract
- test変更 → testing contract
- GLMの自然言語自己分類だけに依存しない。
- 全ruleを毎回全文injectする方式には戻さない。
````

## Amendments

- 2026-08-26 Product boundary: repo固有file一覧ではなく、変更対象・operation category等から必要contractを決定論的に適用するgeneric mechanismをTrack Aで優先する。
- 2026-08-26 Clarification: 本repo固有routingが必要ならTrack Bとして別計上し、A/B両方を評価する。

## Resolved references

- rule activationはworker/reviewerが必要instruction・gateを読む/適用する条件判定を指す。

## External feasibility

status: not-applicable

## Purpose

GLM自身の狭い意味解釈で必要ruleを非適用にできるfailure classを、低固定contextのdeterministic routingへ置換する。

## Contract

- path/change-type/operationからstate/CLI/test等のrule activationを導出するgeneric production境界を設計する。
- 全rule毎回injectを避け、親Codex/Sol追加costを測定する。

## Must not

- prompt自己申告、全instruction全文inject、repo固有path列挙だけで完了しない。

## Acceptance criteria

- 代表operationで必要ruleがGLM自己申告なしに適用され、非該当ruleは定常contextを増やさない。
- F5のA/B分類と追加costを記録する。

## Historical invariants

- lossless task requirementとinstruction read graph削減を維持する。

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE / DESIGN RESOLVED。F4はPR #14 Squash Merge commit `87af3f4700f5dd8220c582efa62f18f561c33c20`としてintegrationへ反映済み。予約済みPR #15 branchはlatest integrationを履歴を保って同期済み。現行WORKER/REVIEWER promptはoptional worker ruleの適用をモデルの「必要時だけ読む」判断へ委ね、workflow prompt builder自体にはrule routing signalがないことを確認した。Track Aは任意repoのbaseline差分pathとoperation phaseからtest/state/config/cache/migration/CLI/language ruleを機械導出する。初回NEW_TASKは未来の変更pathを事前確定できないため、worker call中のinstalled worker-rule Read実績とcall後の実diffを比較し、不足ruleだけを同一worker sessionへ機械注入してreviewer前に再検証させる。decision/fix/reviewer等、既にdiffが存在するphaseではcall前に必要rule本文だけを機械注入する。非該当ruleはpromptへ追加せず、rule選択そのものにmodel自己申告を使わない。現行要件はgeneric Track Aだけで閉じられ、repo固有path routingのTrack Bは不要と判定する。追加Sol/Codex callは0を設計上限とし、追加GLM callは初回workerが必要ruleを未読だった場合だけ発生させる。
