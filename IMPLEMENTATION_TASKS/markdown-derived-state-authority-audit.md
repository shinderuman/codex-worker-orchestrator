# Task: Markdown derived state authority audit

## Original instruction

````text
現在のGit境界

って更新されてなさそうだけど不要なら更新しない運用にするか、ちゃんと更新するかどっちかにするべきなんじゃないの
````

````text
これ以外のMarkdownについても同様に見直すべきなんじゃないの
````

## Amendments

### 2026-09-05

````text
監査して是正しても陳腐化させたら意味がないんじゃないの
````

````text
再追加するとFailするGate作っても陳腐化は防げなくないか
````

````text
これを機械的に是正し続けるのは限界があるんじゃねえのか
````

````text
数ターンごとにやってる監査と同様にMarkdownのチェックをするべきなんじゃないの
````

## Resolved references

- 「現在のGit境界」は`IMPLEMENTATION_PLAN.local.md`末尾に手動記載されていたbranch、完了chronology、実装済み機能、保持boundaryの要約を指す
- branch・HEAD・dirty stateはworktreeごとに異なり、完了証跡はGit / CI / bundle / telemetryが既に正であるため、同節は削除しPlanへ再作成しない方針を親Codexが選択した
- 本taskの「これ以外」はrepository内のtracked Markdown全体を指し、同種の可変状態・完了履歴・machine evidenceの手動複製を監査対象とする
- 「数ターンごとにやってる監査」は`codex-efficiency-reevaluation-checkpoint.md`の最大5 task完了以内のcheckpointを指す。model turn数ではなく既存のtask完了cadenceへ統合する

## Purpose

tracked MarkdownにGit、state、telemetry、生成artifact等から再取得できる可変情報を手動複製して陳腐化させる構造がないか監査・是正する。任意の自然言語を継続的に自動訂正するのではなく、手書きMarkdownの責務を規範contract・不変の設計根拠・losslessな要求へ限定し、決定可能な整合性だけを機械検証する。

## External feasibility

status: not-applicable

## Contract

- repository内のtracked Markdownを対象に、要求の正、恒久contract、必要な設計根拠、例示、可変な派生状態、完了chronology、machine evidence要約を区別する
- branch・HEAD・dirty state、current task status、生成可能な一覧・件数、Gitで回収できる完了履歴、telemetryの手動snapshotをMarkdownへ維持する必要があるか確認する
- 手動更新される可変値は原則tracked Markdownへ置かず、canonical command / artifact / Git locatorだけを示す
- repository sourceから導出され、利用者向けMarkdownへ値を載せる必要がある例外は、canonical inputからのdeterministic generatorを唯一の更新経路にし、再生成結果とのexact一致をgateで検証する
- repository外で独立に変化する値はtracked Markdownへcurrent valueを保存しない。実行時取得commandまたはlive authorityへのlocatorだけを保持する
- 同じ事実を複数Markdownへ複製している箇所はauthorityを一つにし、consumer側は最小参照へ縮める
- 手書きMarkdownは規範contract、不変の設計根拠、losslessな要求・Amendmentsへ責務を限定する。current実装の説明が意味的に陳腐化し得る場合は、継続自動是正の対象にせず削除、実行可能test、CLI help、source locatorのいずれかへ移す
- machine gateは生成物のsource-to-output一致、許可section、参照閉包、schedule closure等の決定可能な性質だけを扱う。自然言語と実装の意味的一致を自動判定したことにしない
- 意味解釈なしに安全に機械判定できない記載を「ownerが随時更新する」運用で残さず、immutableな要求・decisionへ分類するか、current valueを除去する
- Markdown削減がSol/Codexのinstruction見落としと入力tokenを減らす一方、要求復元性・品質判断根拠を失わないことを確認する
- 初回inventoryと是正の完了後は、`codex-efficiency-reevaluation-checkpoint.md`で前回checkpointのGit locator以後に追加・変更されたtracked Markdownと、前回から未解決のauthority候補だけを親Codexが確認する。毎回のrepository全Markdown再読は行わない
- 定期差分確認で見つけた新規問題はその場で無関係な修正へ拡張せず、Go/No-Go後に重複を避けて022より前の独立taskへ固定する

## Must not

- 原要求、Amendments、未完了Acceptance、恒久contract、将来taskが参照する必要なdecisionまで「古そう」という理由で削除しない
- Git履歴へ存在することだけを根拠にcurrent requirementや未完了taskを完了扱いしない
- 全Markdownを同じ保存期間・authorityへ一括分類しない
- 手動更新義務を別Markdownへ移動しただけで完了しない
- 一回だけcurrent treeをcleanにして再導入防止なしで完了しない
- 可変値を残したまま、その文言を再追加できないことだけを陳腐化防止と扱わない
- timestamp、更新日、review checklistだけでfreshnessを保証したことにしない
- arbitrary proseの意味的陳腐化を継続的に自動検出・自動修正できると主張しない
- Markdown authority分類のためだけにgeneric semantic checker、LLM監視、定期全file再監査を追加しない
- generic document frameworkや第二のstate databaseを追加しない
- GLM worker/reviewerにcommitまたはpushさせない

## Acceptance criteria

- tracked Markdown全体について同種の派生状態・重複authority候補をinventoryし、保持・削除・参照化・機械化の判断根拠を残す
- current stateを名乗るMarkdown記載がcanonical sourceと一致するか、再生成可能なら手動snapshotを残さない方針になっている
- tracked Markdownに手動のcurrent valueを残さない。残す生成値はcanonical inputからの再生成結果と一致する
- canonical inputだけを変更して生成Markdownを更新しないfixture、および生成Markdownだけを不整合に変更するfixtureがpre-review deterministic gateで失敗する
- repository外で変わるcurrent stateは値ではなくlive取得方法だけが残る
- machine gateのcoverageと非coverageが明示され、意味的freshnessを保証できない手書きcurrent実装説明が残らない
- 完了後の維持は継続的な自動是正ではなく、可変説明を持たない文書責務と決定可能なgateによって成立する
- 初回監査完了時のGit locatorと未解決候補がbounded evidenceとして特定でき、以後の中間checkpointがMarkdown差分確認を実行できる
- 原要求・恒久contract・必要な設計根拠の復元性を維持する
- 残す可変記載にはownerと更新または陳腐化検出境界が一意に定まる
- 必要な修正、関連test / lint、独立review、Sol最終採否を完了する

## Historical invariants

- Planはtask scheduleと最上位目的を正とし、branch・HEAD・dirty state・完了chronologyを保持しない
- ordinary completion evidenceはGit / CI / bundle / telemetryを正とし、HistoryやPlanへ複製しない

## Dependencies

`IMPLEMENTATION_TASKS/quality-toolchain-preflight-before-model.md`

## Review findings

none

## Current boundary

post-worker recovery、quality-surface continuation、quality-toolchain preflightの後に実行する。
