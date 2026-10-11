# Task: CLI Task-start admissionの履歴確認と回帰修正

## Original instruction

以下はBundle / Task admission URGENT要求から本taskが所有する部分の原文である。共通の原要求全文は `IMPLEMENTATION_TASKS/dogfood-bundle-controller-export.md` のOriginal instructionを参照する。

````text
さらに、削除済み/存在しないcommandを実行した際に新規Task開始pathへ入っている疑いがあります。

今回の
```
glm-worker bundle <task-id>
```

がBundle取得ではなく新規Task扱いされ、その結果repo lockに衝突している可能性があります。

この「未知commandが新規Taskになる」問題は過去に一度対策済みだったはずなので、**新しい対策を考える前に、回帰前の実装・Issue・PR・commitを必ず特定してください。**

特にCLIについては、

> blacklistで既知のretired command名だけを弾く

という修正をしてはいけません。

新規Task開始は、回帰前に存在していた**正規のTask-start admission条件を満たした入力だけを許可するpositive/whitelist設計**へ戻してください。

具体的なwhitelist条件はこの指示から推測せず、Git履歴・tests・Issue/PRから復元してください。

### C. 新規Task admissionのデグレ修正

存在しないcommand / command-like inputが新規Task開始へfall throughする回帰を直してください。

重要:

- retired command blacklistを作って直したことにしない
- `bundle`だけspecial-caseしない
- typo一覧を管理しない

**回帰前に存在したpositive/whitelist Task-start admissionを特定し、そのcontractを復元すること。**

新規Taskは、正規のTask-start syntax / admissionを満たす入力だけが開始できる状態にしてください。
````

## Amendments

### 1. 歴史的contractの追加情報が分からない場合の指示

````text
わからないので分かる範囲で作業しろ
````

### 2. Bundle redesignとの責務分離

````text
## CLI Task-start regressionは別問題

「存在しないcommandが新規Taskとして開始される」回帰修正については、先ほどの指示どおりです。

これはBundle redesignとは分離して調査・修正してください。

特に、

- `bundle` blacklist
- retired-command blacklist
- typo blacklist

のようなnegative admissionへ逃げず、**回帰前のpositive/whitelist Task-start admission contractをGit履歴から特定して復元**してください。
````

### 3. 2026-10 再監査で確認した要求参照の欠落

親CodexのTask化指示:

````text
本Taskが共通原要求を委譲しているdogfood-bundle-controller-export.mdはcurrent treeにない。実装前に実在する一次証拠から必要原文を回収し、Resolved references/Amendmentsへlosslessに固定する。回収できない内容を創作せず、既存の「分かる範囲」の指示と確認限界を親が判断する。一般的なretirementの再発防止はtask-requirement-reference-closureで扱う。
````

## Resolved references

- Amendment 1への質問は、#1036 / PR #1072で確認したcontractが「未知の`--...`先頭tokenを拒否し、通常のfree-form Task入力を維持する」ものであり、別のpositive/whitelist contractを導入したIssue/PR/commitが分かるか、というものだった。
- 原要求全文、GitHub access要件、共通Must not、完了・報告要件は `dogfood-bundle-controller-export.md` のOriginal instructionを明示参照する。Bundle実装は同taskの責務である。
- #1036 / PR #1072、#1297 / PR #1309は一次資料の調査locator。歴史的positive contractが実在したと断定する根拠にしない。

## Purpose

command-like inputが新規Taskへfall throughする問題を、歴史的contractとcurrent正規Task-start ownerに基づくpositive admissionで修正する。

## External feasibility

status: not-applicable


## Contract

- Amendment 3の共通要求参照を実装前に解決する。currentにない参照を読めたとみなして実装・reviewを進めない。既存Original instructionは書き換えない。

repository内部のCLI、admission、testsの回帰修正であり、未検証の外部service成立性を前提にしない。

- current parser/registry/start admission、GitHub Issue/PR/commitと回帰前testsを照合し、実証できる歴史的contractと確認できない要求を区別する。
- 歴史的positive contractを確認できた場合、その正規Task-start syntax/admissionを復元する。
- 確認できない場合は存在したと創作せず、確認済みcontract、探索範囲、current positive entry、互換性に関する未確定点を短いNEEDS_SOL_DECISIONへ返す。「分かる範囲」の指示を架空contractの実装許可へ読み替えない。
- 実装前に正規Task-start入力、command-like inputの境界、既存parent transportへの影響をSolが確定する。
- 正規入力だけがTaskを開始でき、未知commandがexecution/admission/lockingへfall throughしないようにする。

## Must not

- retired-command/bundle/typo blacklistを新設して解決扱いにしない。
- Bundleの旧surface、forwarding、aliasを復活させない。
- free-form入力の歴史を隠したり、未確認のpositive contractを「復元した」と報告したりしない。
- GitHub accessが利用できないまま過去contractを推測実装しない。
- mutation/task executionのlockingを弱めない。
- Bundle export実装、controller recovery恒久化、GLM大量消費改善を本taskへ含めない。

## Acceptance criteria

- 正規の新規Task開始入力がTaskを開始できる。
- 正規admissionを満たさないcommand-like inputと存在しないcommandがTaskを開始しない。
- 特定commandのnegative listに依存しない。
- 回帰前testsを確認し、失われたcoverageを復元する。
- repository lint、full Go tests、vet、build、install-smokeを通す。
- 歴史的導入・回帰のIssue/PR/commit、root cause、semantic boundary、positive admissionの根拠と確認限界、tests、final validationを報告する。

## Historical invariants

- Codex Reductionが最上位目的であり、調査・実装・validationはGLMを正とする。
- Taskの正規syntaxとcommand registryを根拠にadmissionを判断し、blacklistをauthorityにしない。
- current controllerのmutation authorityと正規parent transportを維持する。

## Dependencies

none
