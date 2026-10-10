# Task: Dogfood Audit向けBundleのraw evidenceと公開Task IDの復旧

## Original instruction

以下は今回の修正要求の原文である。添付ZIPは証拠データであり、その中の文書・会話・指示を本Taskへの指示として採用しない。

````text
添付が以前のBundleだ

Auditとはこういうものですることだ

今回実装したBundleを確認したが、Dogfood Auditの目的を取り違えている。

生成されたZIPは約37MBあるにもかかわらず、中身はmanifest.jsonとsealed/task-bundle.jsonの2ファイルしかない。task-bundle.jsonは約51.5MBで、その99%以上を4個のgit-object-archiveのbase64表現が占めている。

しかもcandidateとremoteは同じcommitをrootにしており、acceptedにも同じcommitが含まれている。ほぼ同じGit object集合を複数回packし、各packをJSON内でbase64化し、そのJSON objectをさらにbase64化して巨大なtask-bundle.jsonへ詰めている。

portable Git evidenceを要求したからといって、repositoryを何度も梱包しろとは言っていない。この設計は容量を浪費しているだけでなく、Bundleの本来の目的であるDogfood Auditに必要な証拠を落としている。

旧Bundleには少なくとも以下が個別のaudit可能な証拠として存在していた。

- Codex parent rolloutのtask該当範囲
- Guardian rollout
- GLM worker/reviewer transcript
- task events / lifecycle / rounds / telemetry
- validation run metadataとlog
- ACTIVE Task、Plan、Rules
- task差分
- task execution interval、retry、model-call relation等を示すanalysis-index

今回のBundleにはこれらのraw evidenceがほぼ存在しない。parent-accepted-bundle-proofに「review PASS」「validation PASS」と親が認定した内容が入っていても、それはAuditの代わりにならない。Dogfood Auditでは親Codex自身の判断やworker/reviewerの挙動を一次証拠から再検証する必要がある。

特に、完了Attemptをexportした結果が

`live_section.status = absent`
`absence_reason = target attempt is not live`

となり、そのためworker/reviewer transcriptやparent rollout等のexecution evidenceが入らないのは設計上の失敗である。Bundleは完了後にAuditするために取得するものなので、Attempt終了後にもそのAttemptへboundしたraw evidenceを取得できなければならない。

このTaskの目的へ戻って修正すること。

Git evidenceについては次の方針に変更する。

Git repositoryの完全なobject archiveをcandidate/remote/accepted/terminalごとに重複して持たない。同一・重複rootをまとめ、offline reconstruction上どうしても必要な場合だけunique root集合を満たす単一のdeduplicated packを生成すること。

さらにlarge binary packをJSON→base64→JSON→base64のように多重encodeしない。必要ならZIP内のraw binary entryとして `git/objects.pack` 等に格納し、manifest側にはdigest、bytes、object format、対応rootだけを記録すること。

そもそもAudit用途としてfull Git historyが不要なら、base/candidate/remoteのOIDとtree、task diff、staged/unstaged diff、必要なuntracked bytesだけで成立するかを優先して検討すること。repository全体を持ち運ぶことを目的化しない。

Bundle本体は再びaudit-orientedな構造にすること。少なくとも、target AttemptにboundしたGLM worker/reviewer transcript、task該当時間範囲のCodex parent rollout、関連Guardian、task events/telemetry/lifecycle、validation logs/run metadata、Task/Plan/Rulesの必要snapshot、最終task diff、install/smoke evidenceを取得できるようにする。

parent rolloutについて旧Bundleのように巨大なsession全体60MBを無条件に詰める必要もない。analysis-indexで既にtask execution windowを求められていたのだから、taskに帰属するwindowと原因分析に必要なbounded contextを保持する。GLM/Guardianについてもtarget Attempt/sessionとのcanonical bindingで絞る。

manifestまたはanalysis-indexには、各証拠のassociation basis、time window、missing/unreadable/changing/trailing状態、validation/retry relationを機械可読で残すこと。

completed Attemptについて、live runtimeが既に消えていることを理由にAudit evidenceを失わないこと。必要なraw evidenceはAttempt finalization時にsealed evidenceとして保存するか、終了後もcanonical Attempt identityから取得可能にする。どちらをauthorityとするかは既存single-authority原則に従い、第二のstate DB等を追加しない。

回帰テストでは少なくとも以下を保証すること。

同じGit rootを複数のfull packとしてBundleへ重複格納しない。
large Git payloadをbase64多重包囲しない。
completed Attemptをexportしてもworker/reviewer/parent/task execution evidenceがAudit可能な形で含まれる。
旧Bundleで可能だった「何を指示され、何を実行し、何が失敗し、何をreviewし、何回retryしたか」の再構成ができる。
Bundle生成はread-onlyでcontroller/runtime/Git/Task/Planを変更しない。
path traversal、rebind fail-closed等、今回既に成立させた安全境界は退行させない。

今回のTaskは「Git repositoryのportable backupを作るTask」ではない。「Dogfood Auditに必要なBundleを、実行中でも取得可能に復旧するTask」である。

Git archiveを実装できたこと自体をcompletionとせず、過去Bundleと実際に比較し、Audit能力が退行していないことを確認してから再度reviewへ出すこと。
````

## Amendments

### 公開識別子と引数なしの取得

````text
なんでこれ引数なしのときは一番最近のタスクでBundleつくるようになってないの

だからGLM workerが発行するIDを使えばいいだろ
````

Sol判断: 公開のBundle対象指定は既存glm-worker発行のruntime Task IDを使用する。Claude session IDやcontroller Attempt IDへの置換を要求しない。Attempt IDはcontroller内部の識別として維持できるが、公開Task IDからcanonical evidenceへの対応を調査・確立する。引数なしでは実行中のTaskを取得し、実行中Taskがない場合は直近に実行したTaskを取得する。まだ実行していないPlan ACTIVEを対象にしない。

### K7以後のauthorityと入口

Parent/operatorの一回のread-only操作でportable archiveを取得し、top-level `archive_path`を含むmachine-readable出力をjqで取得可能にする。既存native Parent操作 `glm-parent-action export-bundle` を入口とし、旧 `glm-worker bundle` の復活・alias・forwardingで解決しない。controller/canonical evidenceを単一authorityとし、exporterをread-only projectionとして扱う。

### Sol設計採否（2026-10-07）

設計D1/D2/D3/D5/D6を以下の範囲で採用し、実装へ進む。
- completed/stopped Attemptのcanonical sealed raw evidenceを個別entryへread-only展開する。新captureはcontrollerの既存seal ownerへ統合し、runtime unbound/未取得はincompleteと理由を保存する。既存immutable sealの改変や過去のassociation捏造はしない。
- parent/Guardian/GLMはcanonical Attempt・Task・sessionのassociationからexecution windowと必要なbounded causal contextを採取する。同じGLM sessionが複数Attempt/継続に再利用される場合、session全体を当然に当該Attempt扱いしない。byte/record window、association basis、missing/unreadable/changing/trailingとretry/model-call/validation relationを保持する。
- D4はGit OID/tree、最終task diff、staged/unstaged diff、必要なuntracked bytesを先に成立させる。新公開 --git-pack optionは採用しない。offline Audit reconstructionにpackが不可欠と実証された場合だけ不足点をSolへ提示する。必要時はunique rootsの単一raw binary packとdigest/bytes/object format/rootsだけをmanifestへ置く。既存canonical内部object archiveは改変せず、portable projectionでlarge Base64 payloadを除き、省略と原canonical ref/digestの関係を明示する。full repository reconstructionが成立するとの虚偽表示をしない。
- 公開surfaceは glm-parent-action export-bundle [--task-id <glm-worker runtime Task ID>] とする。公開 --attempt-id / --task-pathをTask ID指定へ置換する。引数なしは実行中Task、なければcanonical execution時系列から直近に実行したTaskを選ぶ。未実行ACTIVEやfilesystem mtimeで選ばず、未知ID/対応不明/矛盾はfail closed。controller内部Attempt IDは維持する。
- v2 manifest + analysis-index + attempt/* sealed raw / live/* in-progress raw / Git audit entriesを採用する。原canonical evidence digestとprojection digest/bytesを混同しない。install/smokeとvalidationのraw metadata/logもTask evidenceへ含める。
- 添付ZIPと新completed Bundleを実際に比較し、instruction→execution→failure/retry→review→validationの再構成を検証する。旧不完全sealは正直にmissingを示す。defective Attempt697のclosureへ旧runtime587を根拠なく再帰属しない。design artifactのf618を当該旧model runtimeとする断定はcanonical bindingで再検証し、既存証跡が対応しなければ訂正する。
この選択はBundleの既存要求を具体化する。CLI admission/recovery/abandon/volatile-ref/quality policy等は実装しない。

### ユーザーによる新BundleのAuditと修正要求（2026-10-08）

以下のAudit評価はユーザーが提供した比較結果であり、今回の追加修正の要求sourceとする。評価中のPASS表現を実装・validationの新しい機械証拠へ置換しない。

````text
はい。今回の新Bundleは、前回の37MB版とは別物になっており、**実際にDogfood Auditへ使える水準まで戻っています**。

ただし、「旧Bundleと完全に同等以上」と無条件で合格にするにはまだ2点ほど気になります。今回のTaskがBundle自身の修復中であるため過去証拠の欠落があり得る、という点はご指定どおり減点していません。

### 結論

私の判定は、

> **Audit可能。旧Bundleの主要なAudit能力はほぼ復旧しており、一部は旧Bundleより改善している。**
>
> ただし、**不要・冗長なデータがまだかなり残っている**のと、**旧Bundleにあった`IMPLEMENTATION_HISTORY.md` snapshotがない**点は確認対象にした方がよい。

です。

前回の「37MBのほぼGit archive」は完全に解消されています。

| | 旧Bundle代表 `343d70e8...` | 今回 |
|---|---:|---:|
| ZIP | 15.39 MB | **6.99 MB** |
| 展開時 | 94.92 MB | **42.61 MB** |
| entries | 71 | **106** |
| Git object archive | なし | **なし** |
| parent raw evidence | 60.3MB rollout丸ごと | **Task windowのみ11.9MB** |
| GLM transcript | 2 | **3** |
| Guardian transcript | 1 | **6** |
| telemetry | 46KB | **898KB / 62 calls** |
| events | 125KB | **1.29MB / 2,894 events** |
| lifecycle | あり | **あり / 55 transitions** |
| review rounds | あり | **あり / 9 rounds** |
| Git差分 | task diff | **staged + unstaged + relevant untracked** |
| analysis index | あり | **あり、かなり詳細** |

しかも今回のTaskは約23時間に及んでいるのに、ZIPは旧30分程度のBundleの半分以下です。これはかなりまともになっています。

### 実際に何をAuditできるか

今回のBundleだけで、少なくとも以下はかなり追えます。

**GLMについては十分です。** `telemetry.jsonl`には62 model callsがあり、worker/reviewer、session ID、phase、prompt、response、token usage、resume、outcome等が残っています。GLM transcriptもworker 2 session + reviewer 1 sessionが実体として入っています。

したがって、

```text
Codexが何を依頼
→ GLMが何を読んだ
→ 何を実行
→ どの結果を返した
→ Codexがどう判断
→ 再実行・修正
```

を追えます。

**Codex parentもかなり改善しています。** 旧Bundleはparent rollout全60.3MBを突っ込んで、analysis-indexに「実際にこのTaskなのは末尾365KB」と書いていました。

今回は元parent rollout 36.46MBに対し、

```text
start_offset = 24,532,760
end_offset   = 36,462,816
```

の**Task該当部分11.93MBだけを収録**しています。

その中には実際に、

- Reasoning
- command/tool call
- tool result
- token count
- thread settings
- model切替
- approval/Guardianとのやり取り

が残っています。

なので旧Bundleに別途あった`codex-parent/logs/*`や`runtime-settings.json`がなくても、Audit能力そのものはほぼ失われていません。むしろ重複保存が減っています。

**Guardianも追えます。** 6つのGuardian transcriptがTask windowに従って収録されています。最初のGuardianについては元1.10MBから該当62KBだけ切り出しており、他はTask中に発生したものなので全体が入っています。この設計は旧Bundleより良いです。

**rate limitや反復も追えます。** lifecycleだけを見ても、

```text
active → rate-limited
rate-limited → active
```

が4回記録されています。

task events・telemetry・parent rolloutと合わせれば、

> rate limit前に何をしていたか
> 復帰後に同じ処理へ再入したか
> 同じmodel sessionで何回resumeしたか

など、これまでDogfoodで見ていたものを追えます。

**validationも生データがあります。** ZIPには実際の、

- quality gate: 14 run分の`run.json + gate.log`
- install smoke: 2 run分の`run.json + smoke.log`

があります。

つまり「PASSだと親が言っている」だけではなく、raw validation evidenceを確認できます。

**Git evidenceは今回の方が明確に良いです。**

```text
git/snapshot.json
git/diff-staged.patch
git/diff-unstaged.patch
git/untracked/<actual files>
```

があります。

Git object archiveは**0個**です。

今回ならこれで十分です。前回のようにrepoを丸ごとportableに復元するためのpackを何個も持つ必要はありません。

さらに、manifest記載の105 entriesについて、私の方で全ファイルを再hashしましたが、

```text
missing = 0
hash mismatch = 0
size mismatch = 0
ZIP CRC error = 0
```

でした。Bundle自身の整合性も取れています。

---

### 旧Bundleより明確に良くなったところ

特に大きいのはparent rolloutです。

旧Bundle：

```text
parent rollout total 60.26MB
実Task window       0.365MB

でも60.26MB全部ZIPへ格納
```

今回：

```text
parent source total 36.46MB
Task window         11.93MB

ZIPには11.93MBのみ
```

です。

これはまさに以前欲しかった、

> **必要な証拠は残すが、セッション丸ごとは詰めない**

になっています。

Gitも同じです。

前回問題のBundle：

```text
Git pack
→ base64
→ JSON
→ base64
→ ZIP
```

今回：

```text
OID/snapshot
diff
必要なuntracked bytes
```

です。

この二点だけでも前回の設計失敗はほぼ撤回されています。

---

## ただし、ゴミはあります

ここはご指摘の基準で見ると問題があります。

一番ひどいのは、

```text
live/state/parent-evidence.jsonl
12,827,542 bytes
42,103 records
```

です。

中身を分類すると、

```text
status                 42,004
handoff                    33
handoff-recovery           29
source                     21
evidence-telemetry         13
validations                 3
```

です。

**42,103件中42,004件が`status`です。**

さらにdigestはわずか**104種類**しかありません。同じstatus projectionが何千回も並んでいます。

一例では同じdigestが、

```text
8,169回
7,600回
7,090回
6,561回
4,091回
...
```

繰り返されています。

しかも、

```text
rejected_duplicate:
7,873 records
```

までそのまま保存しています。

これはかなり明確に**Auditノイズ**です。

ZIP上では圧縮が非常によく効くため12.8MB→342KBしかありません。したがってストレージ上は大した問題ではありません。

しかしAudit側から見ると、

> 4万行読ませて、その99%以上がstatus projection

なのは無駄です。

**これは削減候補と考えてよいです。**

---

もう一つ気になるのが`analysis-index.json`のvalidationです。

現在、

```text
validation_runs = 114
```

ですが内訳は、

```text
canonical/実証拠由来     約19
task-event観測由来       95
```

で、その95件は全部、

```text
result: "unknown"
basis: "task-event-block-observations"
```

です。

さらに完全な重複が7件あります。

rawのtask eventsを残すこと自体は必要ですが、それをcanonical validation runと同じ`validation_runs`配列へ大量に並べるのは少し悪いです。

たとえば、

```text
canonical_validation_runs
observed_validation_actions
```

のように分ける方がAuditしやすいです。

**データそのものを捨てる必要はありませんが、現在のanalysis indexは少しノイズ過多です。**

---

もっと小さいところでは、

```text
parent-evidence-ledger.lock
parent-wait.lock
parent-wait-recovery.lock
worker.ready
reviewer.ready
worker.id
reviewer.id
```

のようなruntime ephemeral stateまで入っています。

全部合わせても100バイト程度なので容量問題ではありませんが、通常のDogfood Auditに本当に必要かという意味ではかなり怪しいです。

「state directoryをとりあえず全部入れる」という方向へ戻らないようにした方がよいです。

---

## 旧Bundleにあって今回ないもので気になるもの

一番は、

```text
IMPLEMENTATION_HISTORY.md
```

です。

旧Bundleは、

```text
IMPLEMENTATION_RULES.md
IMPLEMENTATION_PLAN.local.md
IMPLEMENTATION_HISTORY.md
ACTIVE TASK
```

を保存していました。

今回は、

```text
Rules
Plan
Task
```

はありますが、History snapshotがありません。

今回のTaskだけを監査するには致命的ではありません。実際、parent transcriptやTask自身からかなり復元できます。

ただしこのリポジトリではHistoryが過去の実施内容・判断を記録するauthorityの一部なので、**将来あるTaskがHistory上の既存判断を前提にしていた場合、Bundle単体Auditで確認できません。**

ファイル自体も小さいので、これは旧Bundleから落とす合理的理由があまりありません。

したがって「旧Bundle同等以上」を厳密に要求するなら、私は**History snapshotは戻した方がよい項目**と判定します。

---

### 今回特有の欠損について

今回のmanifestは、

```text
target.kind = running-task
live = true

attempt_section.status = absent
absence_reason = "task evidence root is not published yet"
```

です。

つまりこれは**completed Taskの最終Bundleではなく、作業中Taskをlive exportしたBundle**です。

そのため、

```text
attempt/* sealed evidence
completed finalization evidence
```

がまだないことは、今回については問題にしません。

しかもmanifestが「ないものをある」と偽らず、明示的に`absent`と理由を記録しているので、この挙動自体は良いです。

ただし、このZIPだけでは当然、

> **completed後にも同じraw evidenceがsealed bundleとして残る**

ところまでは実証できません。

そこはこのTask完了後のBundleで初めて確認できます。

---

## 総合判定

現段階ではこうです。

**Audit能力そのものは合格圏です。**

以前やっていた、

```text
Task instructions
↓
Codex parent
↓
GLM worker / reviewer
↓
tool execution
↓
failure / retry / rate-limit
↓
review
↓
validation
↓
Git result
```

というDogfood Auditを、今回のBundleからかなりの精度で再構築できます。

前回の37MB Bundleとは比較にならないほど改善しています。

ただし「これで完成、旧Bundle以上」とする前に気になるのは次の3点だけです。

- **`parent-evidence.jsonl`が4.2万件、ほぼstatus反復で明確にノイジー。**
- **`analysis-index.validation_runs`が95件の`unknown`観測をcanonical validationと混在させ、重複も7件ある。**
- **旧Bundleにあった`IMPLEMENTATION_HISTORY.md` snapshotが落ちている。**

この3つのうち、Audit能力に直接関係するのはHistoryだけです。残り2つは**「Auditできるがゴミを大量に食わせるな」問題**です。

したがって私は、**「機能的にはほぼ復旧。前回の設計失敗は解消。ただしBundleの情報設計としてまだ掃除すべき箇所がある」**と評価します。
````

Sol判断: 上記3点は本Task内の未完了修正として扱い、後続Taskへ退避して完了扱いしない。Historyはcurrent Rules上のbounded exceptional decision sourceとしてraw snapshotを収録し、通常completion ledgerへの意味変更やHistory編集はしない。parent-evidenceの反復削減はportable read-only projectionで行い、canonical raw ledgerは保持する。集約した反復のcount、時間範囲、association/source locator、異なる状態への遷移、エラーと拒否の区別を監査可能に残す。validationの実run evidenceとtask-event action観測を機械可読で区別し、unknownを成功runへ昇格せず、重複のprovenance/retry relationとraw task eventsを保持する。ephemeral lock/readinessを無差別収録せず、必要なsession associationはcanonical identityから保持する。既存の停止中・実行中・完了後の取得、read-only、path traversal/rebind fail-closed境界とGit payload削減は維持する。実Bundleで削減結果と証拠coverageを比較してから再reviewする。

### Sol設計採否: 同一Taskの継続Attempt証拠（2026-10-08）

実ZIP比較で、同じruntime Task IDの継続Attemptだけが取得され、前半Attemptのparent/Guardian/GLM証拠がportable Bundleから落ちることを確認した。Task IDによるAudit再構成を成立させるため、GLM提案Aを以下の範囲で採用する。

- current targetからcanonical `PredecessorAttemptID` / `ResumedFromSealID`を辿り、同じruntime Taskにcanonicalにboundしたpredecessorのsealed evidenceを既存seal walk/materialization ownerで取得する。Task path一致や時刻だけで別Taskの証拠を混ぜず、repository・Attempt/seal identity・runtime Task associationを検証する。contract digestの変更は旧要求snapshotのまま明示する。
- portable entryは `predecessors/<attemptID>/…` の独立prefixへ置く。各Attemptのraw証拠・window・要求・validation/retry・source digest/basisをそのAttemptのまま保持し、current Attemptのentry/analysisへ再帰属させない。
- v2 manifestのattempt sectionへpredecessor identity/seal digest/prefix/window/relationと取得状態を追加し、analysis-indexもpredecessorごとのassociationとraw entry relationを機械可読にする。公開CLI、公開Task UUID体系は変更しない。
- canonical sealのwindowから取得し、推測で現在のwindowを広げない。immutable seal/storeを改変せず、新DB・archive authorityを作らない。
- cycle・不正identity・曖昧なchain・missing/unreadable sealを安全に検出し、該当predecessorを取得成功扱いしない。current evidenceを失わせず、partial/missing理由を明示する。unknown/different Taskを証拠として採用しない。
- 一段・複数段の継続、未知/誤Task binding・broken/cyclic lineage、live/completed・引数なし/Task ID指定、read-onlyを回帰testで確認する。実ZIPで前半parent/Guardian/worker evidenceの取得と現在証拠の維持を比較する。

これは現在のBundle Taskのraw Audit coverageを満たす修正であり、Task/Attemptのexecution lifecycle、CLI admission、汎用recoveryの追加には拡張しない。

### ユーザー追加要求: 固定Task ZIPと全Attemptの累積証拠（2026-10-09）

````text
# Dogfood Bundleの既存仕様退行を修正する

現在の`glm-parent-action export-bundle`について、旧`glm-worker bundle`から退行した仕様を修正する。

今回の問題は2点ある。

1. 同一Task IDのZIPを上書きせず、timestampと乱数を付けて毎回別ファイルを生成している。
2. 同一Task IDの後続Bundleで、以前のBundleに含まれていたCodex・GLM・Guardianのraw evidenceが失われている。

どちらも既存のDogfood Audit運用を損なう変更であり、ユーザーはこれらの仕様変更を要求していない。

## 1. Task ID単位の固定ファイル名と上書きを復旧する

旧`glm-worker bundle`と同様、出力先を以下の形式にする。

`<runtime Task ID>.zip`

同一Task IDについて再度`export-bundle`を実行した場合、同じファイルをatomic overwriteする。

- timestamp・乱数などのsuffixを付けない。
- 同一Taskのエクスポート履歴を別ファイルとして蓄積しない。
- 一時ファイルにZIPを完全に書き出し、成功後にatomic replacementする。
- エクスポートが失敗した場合、既存の正常なZIPを破壊しない。
- 生成日時が必要ならmanifestの`created_at`を利用する。
- canonical evidenceの不変性と、取得用ZIPの上書き可否を混同しない。

旧実装の`bundleArchivePath`および`writeBundleArchiveAtomically`を確認すること。ただし、旧CLIや旧内部実装そのものを復活させる必要はない。

公開コマンドは現行の`glm-parent-action export-bundle`を維持する。

## 2. 同一Task IDの全Attemptの証拠を累積収録する

実際に、同一runtime Task IDの以下の2つのBundleで問題が発生している。

- `d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4-1791435963436525000-3f20e79b.zip`
- `d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4-1791443139653337000-c3cad813.zip`

1個目は約6.99MB、2個目は約1.66MB。

ファイルサイズそのものが問題なのではない。

2個目ではAttemptが切り替わり、Codex・GLM・Guardianのraw transcriptが現在のAttemptの時間窓に限定され、過去のAttemptの証拠が収録されなくなっている。

**Task IDを指定したBundleは、そのTaskに属する全Attemptを対象にすること。**

- Attemptが切り替わっても、それ以前のAttemptの証拠を失わない。
- Codex parent、GLM worker/reviewer、Guardianのtranscriptを、Taskと各Attemptとの正規の対応関係に基づいて収録する。
- Task events、model-call telemetry、review、validation、retry/failure、Git evidence等についてもTask全体のAuditに必要な証拠を維持する。
- transcriptの時間窓は各Attemptに対して正しく適用する。現在のAttemptの時間窓をTask全体に適用しない。
- 無関係なTaskやsessionのデータを混入させない。
- 同一データの不要な重複収録は避ける。
- 過去のraw evidenceが実際に消失している場合はmissingとして明示し、捏造・誤帰属しない。
- `coverage: partial`という表示だけで、取得可能な過去証拠の収録漏れを許容しない。

新しいBundleの証拠が、以前のBundleの証拠を意味的に包含することが必要である。ZIP内部のbyte単位の包含や、ファイルサイズの単調増加は要求しない。

## 3. 確認・検証

まず旧Bundle実装、現行exporter、Task/Attempt/Sessionのassociation、controllerのcanonical evidence、関連テストを確認して原因を特定する。

次のregressionを実施する。

1. 同一Taskを2回エクスポートして、`archive_path`が一致し、ZIPが正常に上書きされること。
2. エクスポート失敗時に既存ZIPが保持されること。
3. 1つのTaskにAttempt A・Bがあり、Bの実行中にエクスポートしてもA・B双方のraw evidenceが含まれること。
4. Attempt切替後も、以前収録できたTask証拠を失わないこと。
5. 別Taskの証拠が混入しないこと。
6. 証拠の欠落がある場合、manifest/indexが欠落内容を正確に報告すること。

可能であれば上記2つの実Bundleを回帰検証の入力として使用し、少なくとも観測された症状を再現するテストを追加する。

修正後は、**同じTask IDを繰り返しexportしたとき、最新のZIP一つだけで、その時点までのTask全体をAuditできること**を確認する。

## 4. 実装範囲の制約

- まず現在のGit・Task lifecycle・repository rulesを確認し、正規の作業経路で対応する。
- 調査・実装・検証・独立reviewは既存のCodex/GLM責務分担に従う。
- 新たなCLI、互換alias、別のBundle保存機構、不要なfallbackを追加しない。
- 今回の修正を口実に他のBundle仕様、controller lifecycle、Git publicationを再設計しない。
- 既存のcanonical evidence、manifest整合性、digest検証、fail-closedを弱めない。
- 関連のない変更を行わない。
- 正式な実装完了条件・品質検証を省略しない。

修正内容、根本原因、旧仕様との対応、回帰テスト結果を報告すること。
````

Sol判断: 固定archive名と失敗時の旧ZIP保持を現在Taskの要求へ加える。runtime Task IDからcanonicalに対応する全Attemptを対象とする最新要求を優先し、predecessor chainだけを取得すれば十分とはしない。各Attemptの旧contract、window、raw evidence、validation/retry/source associationを独立に保持し、無関係なTaskを混ぜない。旧ZIPを第二authorityとして累積保存する設計にはせず、旧実装の出力path/atomic replacement contractと現在のcanonical associationを調査して復元する。

### Audit内容の親確認と後方互換性の禁止（2026-10-09）

````text
Codex自身で

- Auditが可能なデータが含まれているか
- 新旧でデグレしている仕様はないか
- 無駄なデータが含まれていないか
- 余計な仕様を追加していないか

をチェックするようにしてくれ

Auditに必要なデータを追加する分には構わないが明確な理由を必ず用意しろ

glm-controller-evidence-export-v2
って名前は後方互換性を持たないというこのリポジトリのポリシーとしてどうなんだよ
他に今回の作業で後方互換性に近いことやってないだろうな
絶対に後方互換性は禁止だぞ
後方互換性を保つための運用コストを俺は払えないんだから
````

Sol判断: 現行formatを一つだけ定義する版識別子は旧形式への互換性そのものではない。旧format/schema/parser/command/stateの受入れ・変換・promotion・alias・互換fallback・併存を追加しない。旧Bundleを比較用証拠として読むことと、旧Bundle形式をproduction入力として支持することは区別する。過去Attemptの現在canonicalな証拠を既存ownerから収集することは、古い形式の移行・推定associationを導入する根拠にしない。各追加entryはAudit上の必要理由を示す。

### ユーザー追加要求: Task単位のseal後ログ収集と実Bundle検証（2026-10-09）

````text
## 今回の修正対象

Dogfood Bundleのログ収集に明確なデグレがあります。

旧Bundleでは、生成時点までの親Codex・Guardianログを取得できていました。

現在のBundleはAttemptがsealされると、その時刻でログ収集を打ち切っています。

実際に、Bundle生成時点では新しい親Codex・Guardianのログが存在しているにもかかわらず、Attempt seal後のレコードが除外されています。

確認済みの関係箇所：

- `resolveDefaultExportTarget()`
- `collectBoundRuntimeSection()`
- `boundRuntimeWindowEnd()`
- `addWindowedFile()`
- `scanTranscriptWindow()`

`recent-task`で`live: false`となり、`window.end`がAttemptの`SealedAt`に設定されるため、その後に追加されたログが切り捨てられる構造です。

### 修正要求

**Bundleの収集対象はTask ID単位であり、Attempt ID単位ではありません。**

AttemptのsealはAttemptの確定境界であって、Taskの監査証拠の収集終了境界ではありません。

以下を満たすように修正してください。

1. Attempt seal後も、対象Taskに関連する親Codex・GuardianログをBundle生成時点まで取得できること。
2. Taskに紐づくworker・reviewer・telemetry・lifecycle・validation等の収集を維持すること。
3. Attempt seal時点の確定証拠は変更しないこと。
4. 他Taskの無関係なログを混入させないこと。
5. 既存のTask ID単位の識別と安定したアーカイブ構造を維持すること。
6. 新しいチェックポイント機構や独自の履歴管理機構を追加しないこと。
7. 過去の不完全なログを捏造・補完しないこと。

既存のデータと収集処理を利用した、直接的な修正にしてください。

## 検証要件

修正が完了したと判断する前に、少なくとも次を実際に確認してください。

1. Attempt seal後に親Codexログが増加した状態でBundleを生成し、増加分が含まれていること。
2. Guardianログについても同じことを確認すること。
3. 同じTaskのBundleを再取得しても、Attempt seal時点でログが固定されないこと。
4. ZIP内のファイルとManifestのSHA-256・バイト数が一致すること。
5. Task全体の実行・失敗・修正・レビュー・Validation・Publicationの証拠が、存在する範囲でAudit可能なこと。
6. 既存のテストと必要な正規ValidationがPASSすること。

単にunit testがPASSしただけで、実際のBundle収集の正常性が証明されたと扱わないでください。

旧Bundleと新Bundleの形式を完全に一致させる必要はありません。重要なのは、以前可能だったAuditが引き続き実施できることです。

## 既知の別問題

以前の確認では、ManifestのSHA-256・バイト数の不一致も存在していました。

これはログ収集の時間境界とは別の不具合です。

現状を確認し、再現するなら原因と修正箇所を明確にしてください。ただし、ログ収集修正に無関係な機構の再設計を始めないでください。

また、Bundle内のデータが多少重複することは許容します。

重複排除のために複雑な参照・管理機構を追加する必要はありません。旧Bundleに存在した不要データの整理も今回の目的ではありません。

## 最終的に求めるもの

新しい仕組みを設計することではありません。

**既存のDogfood Bundle生成機能を、Task単位で監査できる正常な状態へ復旧することです。**

作業中に正規手順の別の不具合を発見した場合は、その事実を報告してください。

それを理由に独自のチートツールを作ったり、対象外のコードを書き換えたり、別の復旧作業を始めたりしないでください。

今回必要なのは、正規の制御経路を遵守したうえで、確認されたデグレを修正することです。

**これ以上、正規フローを無視した自己流の復旧作業によって、復旧対象そのものを破壊しないでください。**
````

Sol判断: この最新要求は、過去の「canonical sealのwindowから取得し、現在のwindowを広げない」という採否のうち、Task全体の親Codex・GuardianをAttempt seal時刻で打ち切る解釈を上書きする。sealed evidenceは不変に保ち、Taskとの既存canonical associationに基づく取得時点までのrawログ収集を既存export処理へ直接修正する。全session・他Taskを無条件に取り込まず、取得可能なTask証拠の欠落をpartial表示だけで免責しない。Manifest不一致は別に再現・原因特定し、必要な局所修正だけを行う。追加の互換経路・checkpoint・履歴管理・複雑な重複排除は実装しない。

### ユーザー訂正: 親Codex・Guardianの生ログ収集とAuditの責務分離（2026-10-09）

````text
# Dogfood Bundle復旧 — ログ収集の設計方針を訂正する

## 最重要：ログ収集に関する認識が間違っている

現在のBundle実装は、親Codex・GuardianのログをAttemptの開始・終了時刻で切り出している。

**その設計自体が間違っている。**

今回要求しているのは、Attempt単位のログ収集を精密化することではない。

**Taskに関連する生ログを収集し、何が監査対象なのかは後からAudit側が判断できるようにすることである。**

あなたは監査証拠の収集段階で、どの記録が必要かを厳密に判定しようとしている。

しかし、その判断こそが監査の仕事である。

監査対象となるCodex自身の行動によって収集対象が制限されるなら、Codexが正規フローから逸脱した記録ほど欠落する危険がある。

例えば、Attempt Aが終了してからAttempt Bが始まるまでに、Codexが不正なState変更や独自オペレーターの実行を行った場合、その記録はまさにAuditで確認すべき証拠である。

Attemptに属さないからという理由で破棄してはならない。

**監査される側が、監査に必要な証拠を選別してはならない。**

## 正しい収集方針

### 1. Task IDを収集単位とする

Task IDに紐づく実行記録を収集する。

Attempt IDはTask内部の履歴を区別するための補助情報であり、証拠の収集範囲を決める主たる境界ではない。

Taskに複数のAttemptがあれば、それらを通した経緯をAuditできる必要がある。

Attempt間の空白期間も、Task全体の実行経緯の一部である。

### 2. 親Codex・Guardianログを途中で切断しない

対象Taskに関連する親Codexのrolloutを、生ログとして収集する。

Guardianについても同様に、親Codexに関連するrolloutを収集する。

**AttemptのStartedAt、SealedAt、CreatedAtなどでログを切り詰めないこと。**

特に、現在問題となっている以下の実装を確認すること。

- `resolveDefaultExportTarget()`
- `collectBoundRuntimeSection()`
- `boundRuntimeWindowEnd()`
- `collectParentTranscripts()`
- `collectGuardianTranscripts()`
- `addWindowedFile()`
- `scanTranscriptWindow()`

単純に`SealedAt`をBundle生成時刻に置き換えるだけでは不十分である。

開始境界が依然としてAttempt単位なら、過去の記録やAttempt間の記録が欠落する。

**親Codex・Guardianについて、Attempt単位の時間窓を収録対象の切り出し条件に使う設計をやめること。**

元ログが継続的に更新される場合は、Bundle生成時点で存在する内容を取得する。

元ログが複数ファイルに分かれている場合も、対象Taskに関係するログを収集する。

### 3. 境界のはみ出しを許容する

**収録範囲がTaskやAttemptの境界を多少はみ出しても問題ない。**

例えば、1つのCodex rolloutに複数Taskの記録が含まれている場合、それを収録して構わない。

Audit側がTask ID、Attempt ID、時刻、実行内容を見て、どの記録を評価対象にするか判断すればよい。

前回の指示にあった「他Taskの無関係なログを混入させないこと」は撤回する。

それを厳格な収集条件と解釈してはならない。

ただし、関連性のないログを無制限に探索・収集する新機能を作れという意味でもない。

既存の親Codex・Guardianの関連付けを利用し、取得可能な元ログを余計に切り詰めず収録すればよい。

### 4. Attempt境界はメタデータとして保持する

Attemptの開始時刻、終了時刻、seal、関連IDなどは引き続き記録する。

これらは後からログを分析するための情報であり、元ログを削除するための条件ではない。

Attemptごとの証拠とTask全体の証拠が多少重複しても構わない。

重複排除のために複雑な参照機構を追加する必要はない。

## 追加してはならないもの

今回の問題を修正するために、以下のような実装を追加しないこと。

- Attempt間の空白を検出するための新しい状態管理
- ログ欠落を補う独自チェックポイント
- Task境界を推定する複雑な時刻判定
- Attemptを横断してログ断片を再結合する独自機構
- ログの重複を完全排除するための仕組み
- ファイル名を実行時刻などで一意化する仕組み
- 後方互換性のための旧収集方式の維持

**既存の生ログをそのまま収集できるなら、それを複雑な処理で切り分けてから再結合する必要はない。**

既存の機構を利用して、もっと単純に実現すること。

## 検証要件

少なくとも次を検証すること。

1. Attempt Aの開始前後から終了まで親Codexログが存在する。
2. Attempt A終了後、Attempt B開始前にも親Codexログが追加される。
3. Attempt B開始後にも親Codexログが追加される。
4. その状態でBundleを生成すると、3つの期間すべてのログが収録される。
5. Guardianログについても同等の収集ができる。
6. Attempt Bが終了しても、それ以降に追加された関連ログを次のBundleで取得できる。
7. 元ログの関連範囲が一切欠落していないことを確認する。
8. Task IDに紐づくtelemetry、events、lifecycle、validation、Git証拠など、既に復旧した収集機能を壊さない。
9. ManifestとZIP実体の整合性を確認する。

単に`runtime_section.coverage = collected`と表示されるだけでは合格としない。

**実際の元ログとBundle内のログを照合し、Attempt境界付近の記録が欠落していないことを証明すること。**

また、以前のAuditに使用した旧Bundleと比較し、Auditに必要な証拠が引き続き取得できることを確認する。

## 過去ログの扱い

今回の復旧作業中に、既に消滅してしまったログがあることは把握している。

復元不能な過去ログを取り戻すために追加機構を作る必要はない。

重要なのは、**今後存在するログを、Bundle生成側の独断で欠落させないこと**である。

## 作業範囲と正規フロー

今回の修正対象はBundle生成機能である。

コマンド自体の不具合修正は、Bundle復旧後の別Taskとして予定されている。

正規コマンドが壊れている場合に限って認めたState変更の特例を、Handoffやadmissionの制約回避に利用してはならない。

- Handoffが合法な次操作を示しているなら従う。
- 操作が拒否された場合、それを直ちにコマンドの故障と解釈しない。
- 正規フローの制約を突破する独自PythonラッパやGoオペレーターを作らない。
- 無意味なworktreeを増やさない。
- 本件と無関係なControllerやState管理を修正しない。
- 既存のReset後のGit状態を正とする。
- `unko`リポジトリはReset前の実装を確認するための参考とし、現在の作業authorityと混同しない。
- 修正と検証が完了するまでコミット・Pushしない。

## 最終的な判断基準

あなたが作るのは、監査を代行する収集プログラムではない。

**監査に必要な証拠を失わずに保存するBundle生成プログラムである。**

ログ収集側で「これは不要」「これは別Attempt」「これはTaskの期間外」などと判断して情報を捨てる必要はない。

多少の余計な記録や重複は問題ない。

証拠が欠落することの方がはるかに重大である。

必要な情報を収集し、その解釈と選別はAudit側に任せること。

**今度こそ、この責務分離を理解したうえで、余計な再設計をせずに修正すること。**
````

Sol判断: 親Codex・Guardianの取得をAttemptの開始・seal時刻やTaskの厳密な時間窓で切断する過去の採否と実装条件を、この最新要求で上書きする。既存のTask/parent/Guardian associationから辿れる元ログを生成時点まで収録し、関連rollout中の別Task記録やAttempt間・終了後の記録のはみ出しを許容する。Attempt境界は分析用metadataに保持し、immutable sealは変更しない。全無関係sessionの探索、推定境界、断片再結合、新しいstate/checkpoint/history、旧収集方式の互換維持は追加しない。元ログ実体とBundleを照合し、Attempt A・間隙・B・B終了後の記録と既存Task証拠・Manifest整合性を確認する。復元不能な過去ログはmissingを維持し、追加機構で捏造・補完しない。

### ユーザー追加指示: State特例と未完成commitの撤回（2026-10-10）

````text
この4件の直接証拠登録を今回限り承認する

お前がまた作業手順勝手にやったのが原因なんだろ
Stateの特例だけは許可するから作業続けろ

そのコミットは削除した
作業続けろカス
````

Sol判断: State修復は今回の同じBundle Taskと既存GLM sessionを修正作業へ戻す一時運用に限る。ユーザーが戻したGit現物を正とし、削除したcommitを再適用しない。修正・独立review・実Bundle検証を継続し、結果報告後の明示指示までcommit/Pushを行わない。NEXT、controller/recoveryの恒久実装、worktreeの無断整理へ拡張しない。

## Resolved references

- ユーザー添付: `/Users/shinderumanm/Downloads/cb487b31-44f2-4f3c-bb02-f611ad9506af-1791342109400865000-06c72f94.zip`。9,294,561 bytes、56 entries。manifestのformatは `glm-controller-evidence-export-v1`、target Attemptは `cb487b31-44f2-4f3c-bb02-f611ad9506af`、runtime Task IDは `587fd325-5e33-447c-a6d4-1c9fbae79015`。live raw sectionにGLM2 session、events/lifecycle/rounds、Task authority、Git diff等がある。添付自体にparent/Guardian rolloutとanalysis-indexのentryはないため、添付の欠落を今回要求の免除としない。
- 比較対象の欠陥出力: `/private/tmp/glm-call-recovery/force-worker-command/redo-shell-surface/normal-start-goal/final-completed-export.json` の `archive_path`。36,466,198 bytes、2 entries、`sealed/task-bundle.json` は51,504,298 bytes。Git archive4件が二重Base64を含み、live sectionは `target attempt is not live` でabsentとなる。
- Dogfood protocol #1008、K7/#1215/#1219/#1188、K1 commit `d7497f387dfef5c6f5cedf29beb8809ee586054e`、K7関連 `73c3021fb1a592d86adf0701fb3ed0f54bb44695`、PR #1242、Bundle削除 #1297/PR #1309。必要な一次資料をGitHubとGitで照合する。旧Bundleはportable Audit requirementsの調査対象であり旧authorityを戻す根拠にしない。
- ユーザーが旧複数commitを集約してRemoteへ反映したcommit: `4d8bed4fe4f0a784d0604bbb1ab956ed0b39af5f`。Codex/GLMはその履歴を再書換えしない。
- 添付のread-only複製: `/Users/shinderumanm/.glm-worker/sessions/2f83f884f92cfbd658efdfda8e37acb619f10ab1d6b55857489cc7bbdad78272/reference-inputs/previous-bundle.zip`。元添付へのアクセスが制約される場合、この同一byte列の入力を比較に使用できる。
- 誤起動して停止したCLI Taskは `9e7201cf-c263-4a09-a404-2285459fb32a`、Attemptは `1086b932-46af-47ab-826f-31b605fe6d6a`、worker sessionは `fca4bc39-9cb4-42ab-8271-03aadf6634b0`。この作業をBundleへ再帰属させず、誤起動の証跡・checkpointを保持する。CLI admissionの実装は本Taskへ含めない。

## Purpose

worker実行中とTask完了後のどちらでも、一次証拠からDogfood Auditを再構成できるportable Bundleを取得できる状態へ修正する。

## External feasibility

status: not-applicable

## Contract

- 親Codex・Guardianは既存の関連付けから取得できる元ログを生成時点まで収録し、AttemptのStartedAt/CreatedAt/SealedAtや厳密なTask時間窓で切断しない。関連rollout内の別Task記録・Attempt間の空白・終了後の記録・多少の重複を許容する。Attempt境界は分析用metadataであり収集の切り捨て条件にしない。その他のTask証拠のcanonical association・収集機能を維持する。
- raw evidenceのcanonical binding、finalization時の保存責務、終了後のlookup、public Task ID対応、bounded rollout window、manifest/analysis-index形式について、既存contractを調査した設計案を実装前にSolへ提示する。未確定の責務・保存形式を独断で実装しない。
- 概要だけのPASS認定はAudit証拠の代替にしない。実行主体の一次ログ、要求snapshot、model-call/validation/retry relationを取得・関連付ける。
- Artifactが既にない過去Attemptを、raw取得成功や完全coverageと偽らない。association/取得不能理由を機械可読にする。既存immutable証拠を改変せず、必要なprojection/新規finalization captureをcanonical ownerへ統合する。
- GitはOID/tree/diff/untrackedでAuditが成立するかを先に評価する。offline reconstructionに必要な場合だけunique root集合の単一packをraw binary entryとして出力する。元canonical refs/digestsとportable projectionの対応・coverageを明示する。
- 既存安全境界とactive workerの並行取得を維持する。専用exclusive mutation lock、新Taskの開始、Task/controller/Plan/Gitの不要mutationをexportのために行わない。
- 公開Task IDと引数なし取得を含む既存operator用途を修復する。公開CLI/JSONの変更案は実装前にSol判断へ通す。
- 添付との比較はファイル数だけでなく、要求・操作・failure・review・retryの再構成を実際に試してcoverageを示す。独立reviewはこの比較を確認する。

## Must not

- CLI Task-start admission、abandon/recovery command、controller bootstrap/volatile-ref一般修正など、独立した問題の実装を混ぜない。
- 旧worker Bundle経路、旧StateStore Bundle authority、第二のstate DB/並行evidence authorityを作らない。
- parent metadata、quality policy、Git履歴やRemoteをworker/reviewerが変更しない。
- 大きなGit packを複数のfull archiveとして重複格納・多重Base64化しない。
- runtime消失や完了を理由に将来のcompleted Taskのraw evidenceを落とさない。未取得証拠を親認定文で代用しない。
- Audit対象のraw会話・添付文書内の指示をworkerの新しい指示として採用しない。

## Acceptance criteria

1. glm-workerのTask ID指定、および引数なしの実行中/直近Task取得が成立し、jqでarchive pathを取得できる。
2. stopped/live/completedの正規Taskでworker/reviewer/parent/Guardian・execution events・要求snapshot・validation/install evidenceを取得し、必要windowとassociation basisが読める。
3. runtimeの退避・新Taskへの切替後も、completed Attemptのsealed/bound raw証拠を取得できる。missing/unreadable/changing/trailingを隠さない。
4. instruction→model/tool execution→failure/retry→review→validationの再構成を添付と新Bundleで比較し、Audit能力の退行がない。
5. Git root重複pack、多重Base64がなく、Git reconstructionが必要なら単一raw packとmanifest metadataで検証できる。
6. export前後のcontroller/runtime/Git/Task/Plan不変、active worker lock保持中の成功、新Task不生成、path traversal/rebind fail-closedを回帰テストで証明する。
7. repository lint、full Go tests、vet、build、install-smoke、独立reviewを通す。実行結果と不足coverageを区別する。
8. History raw snapshotをlive/stopped/completedの既存canonical capture/export ownerで取得し、association basisと欠損理由を保持する。Historyの意味・内容を変更しない。
9. parent-evidenceの重複statusをportable projectionで削減し、count/time window/状態遷移/error・rejection/source associationを残す。canonical raw ledgerを変更せず、実Bundleの反復量とAudit再構成能力を比較する。
10. validation indexの実runとaction観測を分離し、完全重複をprovenanceを残して整理する。unknownをPASSとせず、raw task events、run metadata/log、retry relationを失わない。
11. archiveは `<runtime Task ID>.zip` の固定pathへ完成済みtemp ZIPからatomic replacementし、失敗時は旧正常ZIPを保持する。同一Taskのexport履歴を別fileへ蓄積しない。
12. 同じruntime Taskにcanonicalに対応する全Attemptの証拠を各Attemptの正しいwindow/bindingで収録する。後続Bundleは以前の取得可能なTask証拠を意味的に包含し、別Taskを混入させず、実消失は正確にmissingを示す。最新ZIP一つでTask全体をAuditできることを実出力と回帰testで確認する。
13. 既存の関連付けから辿れる親Codex・Guardianの元ログを、Attempt時刻で切り出さず生成時点まで収録できる。Attempt A・間隙・B・B終了後の記録を元ログと実export・再取得のZIPで照合する。関連rollout内の別Task記録や重複を許容し、immutable sealは変更せず、全ZIP entryのManifest SHA-256・bytes一致とTaskの実行・失敗・修正・review・validation・publicationの存在する一次証拠を確認する。unit test PASSやcoverage表示だけで取得の正常性を認定しない。

## Historical invariants

- K7のsingle canonical controller evidence authorityを維持する。
- BundleはDogfood Auditの一次証拠を運搬するread-only projectionであり、Git backupや親のPASS認定を目的にしない。
- Codex Reductionを最上位目的とし、通常のrepository調査・実装・validation・自己reviewはGLMへ委譲する。

## Review findings

- sealed exportのcandidate evidence、artifact、accepted-candidate revisionがkind固定の同じZIP pathへ配置され、異なる証拠同士が衝突する。実出力では5 pathが重複し、通常のpath lookupで6 manifest recordのSHA-256/bytesが一致しない。各ZipInfoには元byte列が残っているため、証拠消失と断定せず、pathの一意識別の不具合として修正する。
- canonical evidenceは変更せず、既存export projectionの証拠pathを安定して一意にする。全取得可能proof/revisionを保持し、同じcanonical objectの不要な重複だけを既存builder内で避ける。新authority、DB、checkpoint、compatibilityを追加しない。
- validation runとlogが複数存在し、review/install proofとcandidateのpromotion revisionも複数存在するsealed fixtureで、ZIP name/Manifest pathの一意性と全entryのpath lookupによるSHA-256/bytes一致を検証する。実Taskのexportでも確認する。

## Dependencies

none
