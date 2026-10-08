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

## Historical invariants

- K7のsingle canonical controller evidence authorityを維持する。
- BundleはDogfood Auditの一次証拠を運搬するread-only projectionであり、Git backupや親のPASS認定を目的にしない。
- Codex Reductionを最上位目的とし、通常のrepository調査・実装・validation・自己reviewはGLMへ委譲する。

## Dependencies

none
