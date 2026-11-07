# Bundle取得・CLI Task開始の共通原要求

取得元は `14cd620f9dfccebf23cf92b311db425083ebcddc:IMPLEMENTATION_TASKS/dogfood-bundle-controller-export.md`。下の節は取得元から原文のまま保存した。元コミットは main の祖先ではないため、通常の clone でも回収できるようこのファイルに固定する。

CLIの実装・Task削除は `8fbc190f3b9500cfc9cb04b494392a6077bc7410` で確認できる。CLI担当の原要求とAmendmentsは、その親の `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md` から取得できる。共通原要求に対する本ファイルの固定は、Bundle側の完了やCLIの歴史的admission contractの存在を認定するものではない。

## Original instruction

以下は、同時に受領したBundle / CLI admission回帰修正要求の原文である。本taskはBundle取得と実行中Taskからの取得を所有し、CLI Task-start admissionは `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md` が所有する。

````text
URGENT regressionです。推測で設計しないでください。まずcurrent Gitと回帰前のGit履歴を調査し、既存の正しいcontractを復元してから修正してください。

## 現在観測されている実害

以下の既存Dogfood Bundle取得操作が壊れています。
```
glm-worker bundle <task-id>
```

実行例:
```json
{"error":{"kind":"repo_lock_held","message":"another glm-worker is already running for this repository"}}
```

Dogfoodでは実行中TaskのBundleを取得する必要があります。現在これができず、Dogfood workflowを直接阻害しています。

さらに、削除済み/存在しないcommandを実行した際に新規Task開始pathへ入っている疑いがあります。

今回の
```
glm-worker bundle <task-id>
```

がBundle取得ではなく新規Task扱いされ、その結果repo lockに衝突している可能性があります。

この「未知commandが新規Taskになる」問題は過去に一度対策済みだったはずなので、**新しい対策を考える前に、回帰前の実装・Issue・PR・commitを必ず特定してください。**

## 最初に行う調査

実装開始前に以下を確認してください。

1. current mainのCLI parser / command registry / Task start admission
2. current Bundle / controller evidence / portable archive implementation
3. \#1008 Dogfood protocol
4. K7 / controller cutover関連のIssue・PR・commit
5. Bundle CLI削除に関係したIssue・PR・commit
6. 「存在しないcommandやcommand-like inputが新規Taskにならないようにする」過去の修正履歴
7. その修正がどのcommitで入り、どのcommitで壊れたか
8. 回帰前のTask-start admissionがどのような**whitelist / positive admission**だったか

特にCLIについては、

> blacklistで既知のretired command名だけを弾く

という修正をしてはいけません。

新規Task開始は、回帰前に存在していた**正規のTask-start admission条件を満たした入力だけを許可するpositive/whitelist設計**へ戻してください。

具体的なwhitelist条件はこの指示から推測せず、Git履歴・tests・Issue/PRから復元してください。

## GitHub access

GitHub上のIssue / PR / commit / current repository stateを調査に使用してください。

GitHub accessが利用できず、回帰前のcontractを確認できない場合は、**推測で実装せず停止して、何が確認できないため止まったか報告してください。**

ローカルの記憶や現在コードだけから過去contractを創作しないでください。

## 修正対象

調査結果に基づき、少なくとも以下3件をすべて修正してください。

### A. Bundle取得の復旧

Dogfoodで必要なportable Bundleを再び取得可能にすること。

ただし旧実装をそのまま戻すと決め打ちしないこと。

current controller architecture / evidence authorityと、回帰前のBundle用途を確認して、現在のcanonical architecture上で正しい取得surfaceを復旧してください。

必要条件:

- portable Dogfood Bundleを取得できる
- machine-readableな出力から取得対象を特定できる
- Dogfood監査に必要なraw evidenceを保持する
- current canonical evidence authorityと矛盾しない

### B. 実行中TaskからのBundle取得

Bundle取得は、実行中workerが存在していても可能であること。

Dogfoodでは「処理が終わってから証拠を取る」だけでは足りません。

Bundle取得のためだけに:

- active workerを停止させない
- mutation用exclusive lockを不必要に要求しない
- 新規Task execution pathへ入らない

こと。

ただしlock設計そのものを推測で変更せず、回帰前およびcurrent canonical ownerを確認して正しいread-only境界を使ってください。

### C. 新規Task admissionのデグレ修正

存在しないcommand / command-like inputが新規Task開始へfall throughする回帰を直してください。

重要:

- retired command blacklistを作って直したことにしない
- `bundle`だけspecial-caseしない
- typo一覧を管理しない

**回帰前に存在したpositive/whitelist Task-start admissionを特定し、そのcontractを復元すること。**

新規Taskは、正規のTask-start syntax / admissionを満たす入力だけが開始できる状態にしてください。

## Must not

- 私やユーザーの説明から詳細設計を推測して実装しない
- retired command blacklistを新設して解決扱いしない
- `bundle`だけをspecial-caseして終わらせない
- backward compatibilityのためだけに旧authorityを復活させない
- current controller architectureと並行する第二authorityを作らない
- Dogfood protocolを変更してBundle不要にしない
- Bundle取得のためにactive workerを止める運用へ変更しない
- actual mutation/task executionの安全なlockingを弱めない
- 回帰前contractを確認できないまま「たぶんこうだった」で実装しない

## Regression coverage

回帰前のtestsも確認し、失われているcoverageがあれば復元してください。

最低限、最終的に以下を証明してください。

1. 正規の新規Task開始入力はTaskを開始できる
2. 正規のTask-start admissionを満たさないcommand-like inputはTaskを開始しない
3. 存在しないcommandがTask開始へfall throughしない
4. `glm-worker bundle <task-id>` 相当の正規Bundle取得surfaceがBundleを生成できる
5. worker停止中でもBundle取得できる
6. worker実行中でもBundle取得できる
7. Bundle取得によって新規Taskが作られない
8. Bundle取得によってTask/controller/Plan/Git stateを不必要にmutationしない
9. current canonical evidence semanticsを維持する
10. repository lint / full Go tests / vet / build / install-smokeを通す

## 完了条件

以下3件がすべて実動作で直るまで完了扱いにしないでください。

- Bundleが取れる
- 実行中でもBundleが取れる
- 新規Task開始admissionが回帰前のpositive/whitelist contractへ戻っている

修正後、以下を報告してください。

- 回帰前の正しいcontractを導入したIssue / PR / commit
- regressionを入れたIssue / PR / commit
- root cause
- 修正したsemantic boundary
- なぜblacklistではなく元のpositive admissionを復元したと言えるか
- Bundle exportがcurrent controller architecture上でどこに属するか
- 実行中workerとの並行取得をどう保証したか
- regression tests
- final validation結果
````
