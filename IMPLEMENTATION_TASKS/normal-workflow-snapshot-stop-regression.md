# Task: 通常経路のsnapshot不整合停止の条件差と必要な正規経路の復旧

## Original instruction

以下はユーザーの追加指示の原文である。

````text
多分この次のタスクはいまの先頭のやつじゃなくて「通常経路で不整合停止が再発するなら直す」タスクを積むべきだと思う
そもそもBundleは通常経路が復旧してる前提で作業しているはずなのにさっきから何度もコケまくってるし

今回の5h limit復帰のタイミングじゃコケなかったな
コケるときとコケないときの違いはなんなんだ
それも含めてBundleが終わったらやるタスクにするべきだろう
````

## Amendments

以下はユーザーの追加指示の原文である。

````text
1コミットにしろ

正規経路がないなら正規経路を作れるようにするタスクを用意しろ
それは次のタスクで同時にやれ

1タスク1コミットってのはPlanの書き換えまでを必ずしも含んではいないぞ
1タスクで
a.go
b.go
c.go
の修正が全部バラバラのコミットになるようなクソみたいなことはするなという話だ

Planの変更まで縛ってしまうと今度はPlan開始してないタイミングでタスクを追加、修正することができなくなってしまうだろ
````

1 Task＝1 commitは、同じTaskの実装修正をファイル別など複数commitへ分割しない要求である。Plan更新やTask終了metadataまで同じcommitに含める必要はない。親が追加した「親metadata・実装・Task終了をすべて同じcommitに収める正規経路」の要求は、ユーザーの意図と異なったため取り消す。通常経路での不整合停止の調査・修正と、実際に不足する正規lifecycle経路の確認・整備を、このTaskで扱う。既存の正規経路が使える場合はそれを使い、不足が確認された場合だけ既存canonical ownerの責務内で整備する。現在のBundle Taskの未公開実装・review・validation・install evidenceを保全する。Bundleを完了済みと扱わず、本Taskの登録だけで実行開始したことにも扱わない。

### 2026-10-10の追加要求（原文）

````text
# Dogfood Bundle復旧 — 今回限りの例外を認めて完遂し、後続Taskへ引き継ぐ

## 1. 今回の方針を変更する

現在のBundle復旧Taskを、これ以上コマンド側の不具合修正で停滞させない。

**今回は、既知のコマンド不具合を回避するための例外操作を認める。Bundle復旧Taskを最後まで完遂しろ。**

ただし、今回の例外を恒久的な実装として残してはならない。

本来のコマンド修正は、次のTaskで実施する。

また、Bundle復旧後にもう一度Bundle自体を検証するTaskを設ける。その検証は今回直後ではなく、コマンド修正と別の通常作業を経た後に実施する。

## 2. 現在のBundle復旧Taskを完遂する

対象：

`IMPLEMENTATION_TASKS/dogfood-bundle-audit-restoration.md`

現在のBundle実装については、以下の結果が報告されている。

- 実ZIPのパス衝突：0件
- ManifestのSHA-256・bytes不一致：0件
- 親Codex・Guardianの生ログ収録：確認済み
- Attempt間・seal後の記録：確認済み
- 独立Review：追加修正なし
- lint、vet、build、install-smoke：PASS
- full Go test：PASS
- Validation run ID：`0e93764758c78ab14b0123336f4892a6`

ただし、最後のGoテスト結果は旧Task側のruntimeへ保存されており、現在Taskの正規Validationとして認識されていない。

**Goテスト自体が成功したことと、正規の完了処理が成功したことを混同するな。**

今回確認されたコマンド側の障害は次のとおり。

1. 品質ゲートが`WorkflowConfig`を通らず、canonical runtimeと異なるStateへ証拠を保存する。
2. `finalize-check`がHandoffを重複取得し、`duplicate_parent_projection`で停止する。
3. finalization evidenceの保存先もcanonical runtimeと一致しない。
4. Controller bootstrapがGit refsとleaseのsnapshot不一致で停止する場合がある。

今回は、これらの既知障害を理由にBundle復旧作業を延々と止めることを求めない。

**Bundle本体に未解決の不具合がなければ、例外的な完了処理を認める。**

今回のTaskを完遂するために必要な、一時的なState整合、証拠の参照、Guardの限定的な迂回を許可する。

ただし、以下は厳守しろ。

- 実際にPASSしていない検証をPASSと記録しない。
- 旧Taskに保存されたValidationを、最初から現在Taskで実施したものとして偽装しない。
- 正規コマンドによる成功と、例外操作による完了を明確に区別する。
- 操作前後のGit・State・証拠を保全し、例外操作の内容と結果を記録する。
- 無関係なGuardを恒久的に無効化しない。
- 不要な独自オペレーターや新しい制御機構を追加しない。
- Force Pushや既存証拠の破棄を行わない。

**今回の例外承認は現在のBundle復旧Taskの完了に限定する。後続Taskへ持ち越すな。**

既存の正常な工程は正規手順で実施し、実際に壊れている工程だけを例外処理の対象とすること。

例外があった事実を隠さず、最終的な成果物を正しく検証したうえでTaskを完遂しろ。

## 3. 次のバグ修正TaskのMarkdownを必ず更新する

**ここは今回の最重要指示の一つである。**

次のバグ修正Taskは、新しいCodexセッションで実施する。

したがって、現在のあなたの会話履歴、作業中の推論、一時ファイルだけに情報を残しても意味がない。

次セッションが現在の経緯を知らなくても作業できるように、以下の既存Taskへ必要情報を記録しろ。

`IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md`

このTaskは既にPlanのNEXT先頭に登録されている。新しい重複Taskを作る必要はない。

### 記録必須事項

**A. 今回確定したコマンド不具合**

少なくとも以下を記録すること。

- quality gate専用dispatchが通常の`Execute`を経由せず、`WorkflowConfig`を適用していない。
- `quality_gate_machine_output.go`のState生成が旧Task側へ結び付く。
- `finalization_evidence.go`のclear/saveも同様にcanonical runtimeを参照していない。
- `finalization.go`のrouting確認と最終Handoff取得が二重になり、dedupに拒否される。
- Handoff取得の失敗や不整合を、routing evidenceが存在しないだけの状態として扱う分岐がある。
- controllerとrunnerでvolatile Git refsの取り扱いが異なり、snapshot admissionが失敗する問題がある。

それぞれについて、関係するソースファイル、関数、具体的な原因、実際の失敗結果を記載しろ。

**B. 一次証拠**

少なくとも以下の具体的な記録を残すこと。

- Bundle Task ID：`d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4`
- 旧Task ID：`be0367d5…`（完全なIDは実記録から転記）
- PASSしたValidation run ID：`0e93764758c78ab14b0123336f4892a6`
- `finalize-check`が`status: blocked`となった結果
- `duplicate_parent_projection`のエラー
- evidence batchが`0 runs, routing 0`を返した結果
- `execution workspace snapshot does not match live lease`の拒否
- 実際のRefDigest不一致と、ProjectSnapshot不一致の記録
- 今回の例外操作とその理由・影響

確認済みの原因と、まだ仮説段階のものを区別しろ。

特に、過去の直接State変更が原因の不整合を、すべてController実装の欠陥として扱ってはならない。

**C. 次Taskで実装すべき内容**

今回確認された問題を、既存のcanonical ownerの責務に沿って修正すること。

求めるのは、正規コマンドで通常のTaskを完遂できる状態の回復である。

単に今回のStateを合わせることではない。

quality gate、finalize-check、validation evidence、runtime identity、Handoff dedup、必要なrecovery経路について、実際に確認された欠陥を修正対象に含めること。

ただし、不要な再設計や新しい代替制御機構を追加する理由にはしない。

**D. 回帰テスト・完了条件**

次Taskには少なくとも次の確認条件を残すこと。

- 品質ゲートが現在のcanonical TaskへValidationを保存する。
- `finalize-check`が自分自身のHandoff重複取得で失敗しない。
- Handoffの拒否や不整合を黙って握りつぶさない。
- 他TaskのValidationを現在Taskの正規証拠として採用しない。
- 許可されないGit・State変更は引き続き拒否する。
- stop/resume後も正規のsnapshot admissionが成立する。
- Review、Validation、accept、Publication、Task終了の正規経路が正常に動く。
- 今回のような一時的なState変更やGuard回避なしに完遂できる。

### Markdownの記録品質

**次のCodexセッションに調査をやり直させるな。**

「詳細は前セッション参照」「ログを確認すること」「今回の問題を修正すること」といった抽象的な記述で済ませるな。

必要な事実、再現条件、修正対象、検証条件はMarkdown本文に記載しろ。

証拠ファイルへの参照も残してよいが、`/private/tmp`のような一時パスだけを唯一の根拠にしないこと。

次セッションから確実に参照できるGit上の情報と、保存済みBundle等の一次証拠を明確に結び付けろ。

既存Taskの要求を削除せず、今回確認した内容を適切な位置へ統合すること。

## 4. 三つのBundleを比較する後続Taskを準備する

Bundle復旧後の改善確認を、別Taskとして予約しろ。

ただし、**今回すぐに再監査するな。**

次の順序を守ること。

1. 今回のBundle復旧Taskを完遂する。
2. `normal-workflow-snapshot-stop-regression.md`を新セッションで実施する。
3. その後、Bundle以外の通常開発Taskを少なくとも1件完遂する。
4. その後でBundle再監査Taskを実施する。

Planの既存NEXTの優先順位を不必要に壊さず、この順序を満たす位置に再監査Taskを登録すること。

### 再監査Taskの目的

以下の三つのBundleを比較する。

**Bundle A：今回のBundle復旧Task**

- 長期化した作業
- 複数Attempt
- 親Codex・Guardianの長大なログ
- State不整合と例外操作
- 一時的な完了処理

**Bundle B：次のコマンド不具合修正Task**

- コマンド修正前後の動作
- 品質ゲートと正規Validationの関連付け
- Handoff、Review、Publication、Task終了
- stop/resumeとsnapshot整合性

**Bundle C：その後の通常開発Task**

- 例外操作を用いない通常運用
- 一般的な長さ・構造のBundle
- 実際のDogfood Auditに必要な証拠の収録

三つのBundleについて、Task全体の一次証拠からAuditできることを検証する。

ファイルの存在だけではなく、instruction、execution、failure、retry、review、validation、Git変更、completionを再構築できることを確認する。

また、生成時間や容量についても比較するが、単純なサイズ削減を目的にしない。

**ファイルが重複しているという理由だけで削除を要求しないこと。**

多少の余分な証拠は許容する。重要なのは監査可能性と実装の単純さである。

### 再監査Taskの終了条件

**実質的な問題がなければ、コードを一切変更せずにTaskを終了してよい。**

これは改善実装を必須とするTaskではない。

- 問題があれば、原因と影響を確認して必要な改善を行う。
- 問題がなければ、Findingなし・実装修正なしで正常終了する。
- 容量、命名、コード行数、見た目だけを理由に不要な修正を作らない。

再監査Taskには、この判断基準も最初から明記すること。

## 5. 現在のTaskを完遂するまで、次Taskを開始しない

今回実施するのは以下である。

- 現在のBundle復旧Taskの完遂
- 次のバグ修正TaskのMarkdownへの完全な引き継ぎ
- 後日のBundle再監査Taskの定義とPlanへの登録

**次のバグ修正Taskの実装は行わない。**

次のTaskは、新しいCodexセッションで実施する。

現在のセッションから自動的にNEXTへ進むな。

Task MarkdownやPlanへの変更は、リポジトリで定められたmetadata管理規則に従って永続化すること。

次セッションへ引き継ぐ情報を、ローカルの一時ファイルだけに残して終了するな。

## 6. 最終報告

作業が完了したら、以下を報告しろ。

- Bundle復旧Taskの最終結果
- 実Bundleの検証結果と取得場所
- 今回使用した例外操作と、その理由・結果
- 最終的なGit・Controller・Task状態
- 更新したバグ修正TaskのMarkdown
- 新設したBundle再監査TaskのMarkdownとPlan上の位置
- 次セッションで着手すべきTask

以上を完了させたら停止すること。

## 最重要

**今回の目的は、壊れたコマンドを今回のTaskで無理に直すことではない。**

既に実物検証が進んでいるBundle復旧を完遂し、コマンド側の問題を次のTaskで確実に修正できるようにすることである。

そして、その後に実際の三つのBundleを比較して、必要なら改善する。

改善する必要がなければ何もしない。

**次のセッションに情報を渡せなければ、今回何日もかけて行った診断は無駄になる。**

バグ修正TaskのMarkdownを、次セッションがそのまま作業を開始できる水準まで完成させろ。

今回限りの例外を利用してBundle復旧を完遂し、正規フローの恒久的な修正は次Taskへ引き継げ。

**作業を完遂し、引き継ぎを永続化したらSTOP。NEXTの実行開始は禁止する。**
````

### 2026-10-10 今回の通常Workflow復旧要求（原文のTask固有部分）

````text
## 2. 前Taskから引き継ぐ現在の状態

前セッションではDogfood Bundle生成機能を復旧した。

Git上の結果は次のとおり。

- `8b7556b`：Bundle実装36ファイルをまとめた1コミット
- `c7f7d74`：ControllerのRetireによるTask終了metadata
- `0bc9c25`：本Taskおよび後続Taskへの引き継ぎmetadata

前セッション最終報告時点では、Gitの作業ツリーはcleanでremoteと一致していた。

Bundle実装の実物検証では、ZIP内パス衝突、ManifestのSHA-256・bytes不一致、CRCエラーはいずれも0件だった。

ただし、**ControllerのProject authorityだけがGitの最新状態に追随できていない。**

前セッション最後に引き継ぎmetadataを反映するための既存`adopt`を実行したところ、次のエラーで拒否された。

`external advancement requires suspended or accepted execution authority`

このため、Git上の引き継ぎは完了しているが、ControllerのProject authorityはTask終了metadata時点に残っている。

これは**未解決の既知障害**である。

前Taskがすべて正常完了したと仮定してはならない。

一方、Gitへ反映済みのBundle実装や、既存のReview・Validation・Bundle検証を最初からやり直す必要もない。

まずcurrent GitとControllerを照合し、現在実際に残っている不整合を特定すること。

## 3. 新セッション開始時の特別な扱い

今回は、最初のauthority bootstrapが失敗する可能性を事前に認識している。

前セッションでは、次の拒否も繰り返し発生していた。

`execution workspace snapshot does not match live lease`

通常どおり、まず正規の`glm-worker --authority bootstrap`を実行すること。

成功すれば通常Workflowへ進めばよい。

**失敗した場合は、今回に限り、原因究明に必要なread-only調査を許可する。**

この調査例外で許可するのは、Git、Controller、Runtime、lease、ProjectSnapshot、Handoff、既存証拠および関連ソースコードの読み取りである。

ただし、以下は禁止する。

- bootstrapに成功したものとして処理を続けること
- Stateの直接書換えによって辻褄を合わせること
- 偽のHandoff、lease、ProjectSnapshot、Validationを作成すること
- Guardを一時的に無効化して正常動作したと認定すること
- 過去の例外承認を今回のTaskへ流用すること

**read-only調査を許可することと、通常のmutation admissionを迂回することは別である。**

調査の結果、正規のactivationやrecoveryを成立させるための実装不具合が判明したなら、それは今回の修正対象として扱う。

ただし、現行authorityの下でその実装修正を開始できない場合は、許可されていない操作で突破するな。

必要な追加権限があるなら、関連する制約を一通り確認したうえで、対象・理由・変更前後・検証方法をまとめて報告すること。

**一つのGuardに拒否されるたびに、その場しのぎの許可を繰り返し求める進め方は避けろ。**

## 4. 今回修正する既知の不具合

詳細な原因、ソース参照、一次証拠はACTIVE TaskのMarkdownに既に記録されている。

それを正として確認すること。前セッションの会話履歴を要求する必要はない。

特に、以下の修正範囲を取りこぼすな。

### A. Canonical runtime identityの不一致

品質ゲート専用dispatchが通常の`Execute`を迂回するため、`WorkflowConfig`が適用されず、Validationが別TaskのRuntimeに保存される。

前Taskでは、PASSしたGoテストが旧Task側に保存され、現在Task側ではValidationを取得できなかった。

品質ゲートの実行、子process、status/result/watch、finalization evidenceのclear/saveが、正しいcanonical runtimeを使用するように修正すること。

他TaskのValidationを現在Taskへ付け替えて解決してはならない。

### B. `finalize-check`のHandoff重複取得

`finalize-check`はrouting確認と最終検証でHandoffを重複取得し、`duplicate_parent_projection`によって停止する。

重複取得を解消し、検証後に必要なfresh Handoffを正規のownerから扱えるようにすること。

また、routing用Handoffの取得失敗や不整合を、単なる証拠不足として握りつぶして品質ゲートを実行する分岐も修正すること。

### C. ControllerとrunnerのGit refs認識の不一致

Controllerとrunnerでvolatile Codex refsの扱いが一致せず、lease snapshotの比較に影響する問題が確認されている。

volatile refsの増減と、実際のauthorityを変更するGit操作を区別すること。

実際のHEAD、index、worktree、非volatile refsの変更を安全に拒否する性質は維持する。

比較を全面的に緩和して解決してはならない。

### D. Publication・Retire・metadata同期の不整合

前Taskでは、accept/seal後のHandoffが必要なPublication操作を適切に示さないケースがあった。

また、PublicationとRetireのpush処理がGuardの期待するrefspecと一致しない問題も確認されている。

さらに、Retire後の親metadata更新でGuardが拒否し、最終的には`adopt`も拒否された。

今回の修正では、次を確認すること。

- accept/seal後の合法なPublication操作がHandoffに正しく現れる。
- 通常のPublication・Retireが正規Guardの下で実行できる。
- Task終了後の親metadata更新が正常に扱われる。
- Gitが外部で進んだ状態を、既存のauthority・admission条件に基づいて正しく処理できる。
- 正規のadopt/recoveryが不足している場合は、その不足を適切なownerで解消する。

ただし、**前セッション最後の`adopt`拒否だけを根拠に、ControllerのGuardが間違っていると断定してはならない。**

実際のState不整合、合法操作の欠落、コード上の回帰を区別して判断すること。

## 5. 実装方針

今回のTaskは、既知不具合の調査だけでなく、正常なWorkflowの復旧までを目的とする。

既存の正規ownerと責務を維持し、必要な実装を行うこと。

**問題のある操作を通過させるためだけの対症療法ではなく、通常運用で同じ問題が再発しない修正を行え。**

一方、無関係な機能の追加や再設計は行わない。

特に以下を守ること。

- 恒久的なGuard無効化を行わない。
- 新しい独自State DBや復旧オペレーターを安易に追加しない。
- 過去のState修復スクリプトを恒久機能として採用しない。
- 後方互換のための旧コマンド、alias、fallbackを追加しない。
- GLMへ親metadata編集、commit、pushを任せない。
- テストを削除・弱体化してPASSさせない。
- 関係のないBundle exporterの改善を始めない。

**1 Taskの実装修正は1コミットにまとめること。**

ただし、PlanやTaskのmetadata更新、Task終了処理まで同じコミットに入れるという意味ではない。

既存の正規commit・Publication・Retire手順を維持すること。

## 6. Validationと完了条件

コード変更後は、実際の正規コマンド経路で検証すること。

モックが常に成功する単体テストだけでは不十分である。

少なくとも次を確認する。

1. 現在Taskの品質ゲート結果が、現在Taskのcanonical runtimeに保存される。
2. `finalize-check`が自分自身の重複Handoff取得で停止しない。
3. Handoff拒否・不整合、別Task Validation、snapshot不一致は正しく拒否される。
4. volatile refsが変化しても、正当なexecution authorityが維持される場合は不必要に停止しない。
5. 実際のGit authority変更は、従来どおり安全に検出される。
6. Review、Validation、accept、Publication、Retireを正規手順で実行できる。
7. Task終了後のGit・Controller・ProjectSnapshot・metadataが整合する。
8. 次のTaskを正常に開始できる状態まで復旧できる。

必要なfull Go test、lint、vet、build、install-smoke、独立Reviewも既存規則どおり実施する。

前TaskのPASS結果を今回の実装のValidationとして転用してはならない。

**今回の最終的な成功条件は、一時的なState修復やGuard回避を使わなくても、通常Taskを正規フローで完遂できることである。**

単にunit testが通っただけでTaskを完了扱いするな。

## 7. 既存の引き継ぎ情報を無駄にしない

ACTIVE TaskのMarkdownには、前セッションで行った調査結果と一次証拠の所在が記録されている。

そこには以下も含まれている。

- 発生した具体的なエラーメッセージ
- 関連ソースと責務
- 過去のRefDigest・snapshot不一致
- 過去の直接State変更で生じた不整合
- 品質ゲートの保存先不一致
- Publication・Retire時のGuard拒否
- 以前実行された例外操作の記録

**この記録を読まずに、同じ調査を最初から繰り返すな。**

新しい検証が必要な場合は、何が未確定で、既存の証拠では何が証明できないのかを確認してから実施すること。

なお、Task Markdownにある古い「Bundle未公開」「次Taskは未開始」といった記述は、作成時点のhistorical contextである可能性がある。

現在のGitとControllerの事実を優先し、古い記述を現在の状態として扱うな。

必要な場合はderived ContractやResolved referencesを更新し、Original instructionや過去Amendmentを削除しないこと。

## 10. 最重要指示

前TaskはBundle生成機能を復旧するために、何日もState、Guard、Publication、Validationの問題へ巻き込まれた。

その結果、限定的な例外を何度も使用することになった。

**今回のTaskは、そのような例外が不要な通常運用を回復するためのものである。**

したがって、今回もGuardを迂回して完了させたのでは、本来の目的を達成していない。

問題があるなら既存の正規ownerを修正し、正規の手順で検証しろ。

前セッションの失敗を隠すな。過去のStateを勝手に改変するな。確認済みの調査を無駄に繰り返すな。

必要な修正を実装し、正規のValidation、Review、Publication、Task終了まで完遂すること。
````

### 2026-10-10 起動阻害の直接修正の限定承認（原文のTask固有部分）

````text
今回のACTIVE Taskに限り、正規Workflowの起動を阻害している既存ownerのソースコードと回帰テストについて、Codex自身による直接修正・ローカル検証・必要な通常installを許可する。

対象は以下の問題に限定する。

1. Retire後、実行Taskが存在しない正常なController状態でbootstrapが拒否される問題。
2. 同じ状態でHandoffが不整合となり、合法なTask開始操作を提示できない問題。
3. Retire後に親metadataだけが正当に更新された際、ControllerのProject authorityを同期できない問題。

**実行Taskがない状態を異常と決めつけず、Retire後・次Task開始前の正規状態として扱えるように修正しろ。**

特に今回のGit HEAD `0bc9c25`とController integration tip `c7f7d74`の差分について、実際に親metadataだけの変更であることを検証し、合法な同期経路を既存owner内に成立させること。

任意の外部Git変更を無条件にadoptできるようにするな。

旧terminal record、Controller履歴、証拠、Guardの安全性を維持し、新しいauthorityへの正規遷移を記録しろ。

State直接編集、偽のExecution Task・lease作成、Guard無効化、独自復旧オペレーターの追加は禁止する。

今回の直接修正は、**通常のGLM Workflowを起動可能にするためだけの限定的な例外**である。

修正後は、実際にbootstrap・Handoff・Project authority同期を検証し、正規Workflowが利用可能になった時点でCodex自身による直接実装を終了すること。

その後、ACTIVE Taskに残る既知不具合の修正・独立Review・Validationは通常どおりGLMへ委譲し、Publication・Retireまで完遂しろ。

直接修正部分も今回のACTIVE Taskの実装に含め、最終的に1つの実装コミットにまとめること。途中の検証やinstallを理由に実装コミットを分割するな。

また、今回の不具合そのものが、通常のTask開始を不可能にしていた原因である。**同じ状況でユーザーの追加介入を必要としない正規経路を完成させることを完了条件に含めろ。**

想定外の拒否が発生した場合は、関連する起動・完了経路を包括的に確認し、単発のGuard回避を繰り返すな。
````

### 2026-10-10 旧execution laneの限定直接修正許可（原文）

````text
今回のACTIVE Taskに限り、旧execution laneが正規GLM開始を阻害している問題について、既存ownerのソースコード・回帰テストの直接修正と、必要なローカル検証・通常installを許可する。

これは前回許可した「正規GLMへの委譲を成立させるための直接修正」の延長である。

ただし、**旧laneを強制削除して起動を通すだけの対症療法は禁止する。**

まず、旧laneのindex 32件・worktree 9件について、sealとの差分、workspace identity、所属Attempt、Controller履歴を確認しろ。

既存のcanonical evidenceに保全済みの内容と未保存の内容を区別し、未保存データがある場合は、staged/unstagedの区別を含めて復元可能な形式で保全すること。

そのうえで、既存ownerの責務内に以下を成立させろ。

- live executionやpending transitionを誤って解放しない。
- cleanup対象を正確なlane identityに限定する。
- 実データの保全と復元可能性を検証する。
- 保全済みの状態とcleanup直前の状態が食い違えば停止する。
- cleanupと再利用の結果をControllerの正規証拠として記録する。
- 中断・再実行時にもデータ損失や二重cleanupを起こさない。
- 次Taskを通常のGLMコマンドで開始できる。

seal不一致を無条件に許容する変更、Guard無効化、汎用的な強制削除機構の追加は禁止する。

また、今回の原因が過去の一時的な例外操作による残留物なのか、通常Workflowでも発生する不具合なのかを区別しろ。不要な恒久機構を作るな。

**自動承認審査で拒否された操作を、別コマンドや設定変更によって迂回することは許可しない。** 実際の削除操作に別途承認が必要な場合は、正確な対象と復元手段を提示し、必要な承認を受けること。

直接修正の目的は、あくまで正規GLM開始の成立である。

bootstrap・Handoff・Project authority同期は既に成功している。これらの処理を不要にやり直すな。

旧laneの問題を解消し、GLMへの正規実行要求が成立した時点でCodex自身による直接実装を終了すること。

その後は、残りの実装・テスト修正・独立Review・ValidationをGLMへ委譲し、ACTIVE Task全体を正規に完遂しろ。

実装修正は最終的に1コミットにまとめる。NEXTは開始しない。
````

## Resolved references

- Bundle Taskの通常経路の証拠: glm-worker runtime Task ID `d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4`、canonical Attempt `233b6799-ca36-491a-9df5-a9c1630a0f91`。Taskのtelemetry/events、controllerのmodel-call/lease/mutation/failure証拠、completed Bundleを一次資料とする。
- 保存済みの今回限りの運用State操作receipt: `/private/tmp/glm-bundle-audit-state-switch/`。実結果と一時的なState操作を区別するための入力であり、恒久的な復旧機能を追加する根拠として扱わない。
- 検出surfaceは通常glm-parent-action decision/fix/resumeの実行、およびworker-auto-fix/reviewer入口のparent metadata checkに現れた `execution workspace snapshot does not match live lease` / `parent_metadata_active_unresolvable`。後者の文言だけでmetadata内容が改変・欠損したと断定しない。
- 公開前のreadonly previewは `base_oid=4d8bed4fe4f0a784d0604bbb1ab956ed0b39af5f`、`snapshot_id=c71812a685d0ce8f76efca9b3e258ccccec2f504dc675eed5b1b0383eaefb325`、`tree_oid=5c5704a61526fc431711a4bd82fcadb7764c00b8`。実装25ファイルを含み、準備済みの親metadataを含まなかった。実装を1 commitにまとめ、metadataを別commitにすること自体はユーザーの要求に反しない。
- 正規経路の確認対象: `glm-worker/internal/app/controller_publication.go`、`glm-worker/internal/controller/publication_acceptance.go`、`glm-worker/internal/controller/terminal_planning.go`、`glm-worker/internal/controller/publication_adoption.go`、`glm-worker/internal/controller/publication_mutable_adoption.go`。既存retire処理がTask終了metadataを別commitにすることだけを不具合や経路不足と扱わない。
- 1 commit指定後の再開bootstrapでも `authority bootstrap: resolve controller execution task: execution workspace snapshot does not match live lease` が発生した。直前のacceptは成功していた。原因は未確定で、readonly previewが原因だと推定しない。

### 次セッション向けの確定した再現条件・原因・証拠

今回の実装を読み直す入口はGitの`8b7556b1b47cdc85fd101b13c83d2c0e60ecc66f`（Bundle実装36ファイルを1 commit）と、その親`e3f8dec1460dfb52f076994cc0ec9599801309ce`（Reset後の実在する親metadata）。Bundle復旧Taskの原要求はこれらのcommitにある`IMPLEMENTATION_TASKS/dogfood-bundle-audit-restoration.md`から取得できる。終了metadataはcontrollerのretire transition `be522186-324b-4f01-8a37-1238eb089f42`が生成した`c7f7d741727d8a41d5b1b7888a6e968e1a885991`。実装修正とmetadata commitを区別する。

一次証拠の永続locatorはruntime Task UUID `d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4`の固定ZIP、およびcanonical controller evidence。保存先は`~/.glm-worker/exports/f6189a2ef4b9698296980938dcb3f04542eadb0638397e403f7925130b92cfc4/d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4.zip`。`glm-parent-action export-bundle --task-id d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4`で再取得する。ZIP内のTask artifact `completion-exception/`とanalysis-index/manifestから以下のrecordを特定する。元artifactは`~/.glm-worker/sessions/2f83f884f92cfbd658efdfda8e37acb619f10ab1d6b55857489cc7bbdad78272/artifacts/d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4/completion-exception/`。一時ファイルを唯一の根拠にしない。

- Controller identity: `5156de56d87a719a79cdda90e93a63aebfc6e562d085eea527b652f058d0e3e2`。現Taskのcanonical runtime keyは`2f83f884f92cfbd658efdfda8e37acb619f10ab1d6b55857489cc7bbdad78272`。raw repository-path hashで選ばれた別runtime keyは`f6189a2ef4b9698296980938dcb3f04542eadb0638397e403f7925130b92cfc4`。
- 別runtimeに保存された元Go runはTask `be0367d5-2c3c-409c-929b-d8fff6c07545`、run `0e93764758c78ab14b0123336f4892a6`、実結果PASS。元`original-go-run.json`と`original-go-gate.log`はUUIDを変更せず保存した。同じHEAD/index/worktreeの一致を採用根拠としたが、現在Taskで正規保存したValidationだとは扱っていない。例外の参照根拠は`03-validation-association.json`と`05-evidence-registration.json`。

#### 1. 品質ゲートとfinalization evidenceのruntime identity

確定したsource boundary: `glm-worker/internal/app/machine_output.go:dispatchMachineOutput`の品質ゲート専用dispatchは通常`app/execution.go:Execute`より前にreturnする。通常Executeは`controller/workflow_binding.go:WorkflowConfig`を適用し、RepoHashをrepository lineageとTask pathから束縛する。一方`app/quality_gate_machine_output.go:dispatchQualityGateMachineOutput`はraw configのRepoHashでNewStateStoreを生成する。現Taskのfull Go PASSはこのため別runtimeの旧Taskへ保存された。

`parentactioncmd/finalization_evidence.go:managedFinalizationState`もconfig.LoadからAttachStateStoreへ進みWorkflowConfigを通らない。clear/saveと品質ゲートの保存先をcanonical runtimeへ揃えるべきであり、他TaskのValidationの再帰属で直してはならない。`finalization-evidence-result.json`のevidence batchは`0 runs, routing 0`を返した。テスト実施の失敗ではなく保存・取得ownerの不一致である。

#### 2. finalize-checkのHandoff二重取得と拒否の握りつぶし

`parentactioncmd/finalization.go:finalizationRoutingEvidenceCandidate`がcollectFinalizationHandoffを品質ゲート前に呼び、`runFinalizationCheckWithWorker`が品質ゲート/lint後に再取得する。同一projectionのdedupにより`duplicate_parent_projection`へ入る。実記録`finalize-check-closure.json`はGo PASS後に`status: blocked`、digest `d836…`のduplicate_parent_projectionを返した。同じテストを何度も再実行する必要はない。

routing側にはHandoff取得の失敗・不整合をrouting evidenceの欠落としてfalseへ落とし、通常module CWDから品質ゲートへ進む分岐がある。拒否をそのまま停止結果へ伝え、拒否時はgateを実行しない契約を確認する。fake workerが常に同じHandoff JSONを返す既存testsだけで実際のdedupとcanonical runtimeを証明してはならない。

#### 3. RefDigestとvolatile refs

`controller/identity.go:captureRefs`はgit for-each-refの全refをRefDigestへ含める。`runner/git_authority_guard.go:filterGitAuthorityRefs`と`runner/git_authority_volatile_refs.go:IsVolatileCodexDesktopRef`にはrefs/codex/turn-diffs/とrefs/codex/snapshots/を除外する既存境界がある（add/update/delete testsも存在）。実authority refの比較は維持し、両ownerの意味の相違だけを正す。

拒否は`execution workspace snapshot does not match live lease`。比較は`controller/admission.go:validateLiveLeaseBinding`のsnapshot.Headとlease.ExpectedBaseOID、およびsnapshot.IDとlease.ExpectedWorkspaceSnapshotID。snapshot.IDはHEAD/index/worktree/RefDigestを含み、Attempt初期snapshotとは別である。

記録447→448（mutation `469e201e-d23f-4c43-9c29-40976d2b36a3`）と診断時はHEAD/index/worktreeが一致した。RefDigestは`4db0668c1f390486e09b39c92f20bcd2a6e7046cb1b3a71d3b49aac667753af8`→`7458b542cd24586a27c4b021569ac437aacb61dc3ab996d1c18bff058a5cacb0`、snapshotは`ef3826d72ec158a9592eccd2b65e3d9e760d955380498c9a1bbedc63ad9cce56`→`2c6b68700e112f57555a574573432af188390b9ed1fa8a2087a993137bdd61c0`。全文とGit/leaseの具体値は永続artifact `historical-state-diagnosis.md`。

完了直前g465でもHEAD e3f8dec、index `3ed99c816270857e3458d67d33ab791667f2508793a24f5ff1841668f78154bc`、worktree `01d268504385030b9aa5a12b8d0ec7aefe273583c4527016f8cc08ab3e195fba`は正規mutation `0c3b4039-fef2-47d1-8a39-3c9536b7c249`のafterと一致した。RefDigestは`e7296d6d22309b57cd56135353a08939c912d60d15b6cd1280c53645d340592f`→`2e6c6eaff26c9d32399e700ad975603b9a3da59b034c77721dce50b4663b707e`、snapshotは`ad5fcd4fe531a2b23db0640ff603f77b2bee2b49d8e48cb74877565bfdd8800e`→`13526f308dd86f9592745d1874170b2fc5bd03e455a95a3eaa4250eb284005da`。

確定事項はRefDigest差分とownerの分類差。過去のref集合全体が保存されていないため、全変化がvolatileだったことや変更者まで証明したとは扱わない。g465の例外再束縛は`01-ref-lease-rebinding.json`（旧lease df295bae…保全、新lease16401d7c…、generation465維持）。次Taskの正常経路へ持ち越さない。

#### 4. 過去の直接State変更で生じたProjectSnapshot不整合

ProjectSnapshot `c39f2b42a9115e5ca56c3fc5fc6d37bb64f1278fa0927b7f26f13ffc070f1ee5`は内部digestが正しく、source eebb45b7d48bbf8e093d496ac72458afdbce4c57と旧Task digest ca243c5d07479889df0d9774bddc26578d7e659e1720845d990294e3dddf8ee1のhistorical recordだった。

10/09 14:51 g397→398の直接修復はGit authorityをb01148bbbb31bb7759c2a367ea42367f8a925ba5へ動かしたがProjectSnapshotを残した。14:56 g398→399はTask digestを2bda8099…へ動かしたがTask corpusを残した。17:19 g408→409と10/10 10:28 g439→440も同じProjectSnapshotを残した。これがProject authorityの乖離原因であり、ControllerのJSON破損やvolatile ref問題とは別。

親rollout `rollout-2026-10-06T01-08-40-01a10cd3-0e16-71a2-998b-d3aa8317a0c9.jsonl`のcall/result行11672/11675、11758/11762、11993/11998、13437/13443と保全receiptが根拠。今回承認されたmetadata commit e3f8decから実Git/blobを用いて新ProjectSnapshotを生成したが、Attemptのexecution_base_oidとbaseline_archiveにはb01148が残っていた。例外`02-metadata-base-reanchor.json`はproduction diffのない4metadataの差だけを検証し、既存capture-git-archive ownerでe3f8dec＋元baseline treeをcaptureして当該2参照を修復した。旧Attempt/ProjectSnapshot/archiveは保持した。新Gitで古いreviewを実施済みと偽装していない。

#### 5. seal後のHandoffとPublication

実candidate `83ffd4f9dfc63599bc7c5594e17c0f95632096c4333dd8f9b735144af0c22479`のaccept/seal後、Handoffはcontroller-execution/evidenceだけを示した。`app/parent_handoff_controller.go:canonicalQuiescentAction`はpendingなし・ExecutionTaskRefなし・MetadataLineageあり等のstart条件以外をcontroller-executionへ返し、accepted_candidate_refとpending_terminal_task_refに必要なPublication操作を表さない。実record `quiescent-publication-handoff.json`。今回の`06-publication-handoff-exception.json`は正常Handoff admission成功を主張せず、既存typed Publication ownerだけを使う限定例外を明示する。

`controller/publication_publish.go:pushAndObserveCandidate`と`controller/terminal_apply.go:applyTerminalMetadataPublication`はraw commit OIDをpush元に渡す。`controller/publication_guard.go`のLocalRef条件はrefs/heads/を要求するため、native publishは`invalid publication push update`／`publication push rejected: refs/heads/main`。production commitはpending authorityを保ったまま`glm-publication-guard push-update`で通常branch refspecを確認し、`git push origin main:main`（hook維持）→既存controller-execution recoverで進めた。transition `259946b5-b9fa-4012-b1b9-2f5715e9f42b`、record `normal-controller-publish-recovery.json`。

retireはremote pushをlocal ref移動前に行う同じraw-OID問題がある。今回だけper-process GIT_CONFIGのhooksPath=/dev/nullを指定して既存typed retireを行った（永続設定・hookファイルを変更せず、Force Pushなし）。記録`07-terminal-publication-exception.json`と`normal-controller-retire.json`。正規Guard成功とは扱わない。

promote後に9ファイルがstaged/unstaged両方へ現れたが、worktree bytesは公開HEAD8b7556bと一致し、古いindexとの差分が相殺していた。既存promoteのref移動とindex後処理の境界を次Taskで確認する。現象だけから常にソース不具合と断定しない。今回の整理はその9ファイルだけgit addし、ソース変更・追加の実装commitなしでcleanへ戻した。

retire後のparent maintenanceでは、通常pre-commitが`parent continuation metadata guard rejected inconsistent handoff: repository root is unavailable`で拒否した。実コマンドは親metadata 3ファイルだけのgit commit、元結果はartifact `metadata-normal-commit-rejection.log`。`parentactioncmd/continuation_gate.go:executeContinuationGate/loadContinuationGateHandoff`がraw configとAttachStateStoreを使用することは確認したが、この拒否の全因果は未確定である。次Taskでcanonical runtime bindingとの関係を確認し、正常parent maintenanceとTask終了後の更新を例外なしで扱う。今回のmetadata commit/pushは既知の完了障害に対する1回限定のhook例外を明示して行い、通常Guard成功とは記録しない。

### 追加Contract（以前の要求との関係）

この追記で、古い「Bundle未完了のpublicationを保全する」という一時的記述は、保存済みBundleのhistorical evidenceを保全する要求へ更新する。本TaskはBundle完了後の新セッションで実施し、前セッションのState/Guard例外を引き継がない。次セッションの調査は上記確定境界と未証明点に限定し、同一Go snapshotの再試験や証拠の所在探索を最初から反復しない。

- runtime identityは品質ゲート、run/status/internal取得、finalization clear/saveでcanonical ownerへ一致させる。別TaskのPASSを正規採用しない。
- Handoff拒否・不整合を黙って欠落へ変えない。routing拒否時は品質ゲート開始前に停止する。正常なfinalize-checkは自分の二重取得でdedupへ入らない。
- accepted/promoted/observed/pending-terminal/quiescentの合法操作を既存ownerの実状態に合わせてHandoffへ示す。Publicationのpush refspecはGuardの実contractへ合わせ、不正pushを許可しない。
- volatile refのadd/update/deleteと実authority ref/HEAD/index/worktree変更を区別する。admission・mutation証跡・lockingは維持する。
- Review→Validation→accept→promote→publish→retire→次Taskの正規admissionを、今回のState変更やGuard例外なしに通す。必要なrecoveryが既存ownerで欠けると実証した場合だけ、その責務内の最小修正をSol判断へ提示する。
- 1 Taskの実装修正は1 commit。Plan/Task metadataを別commitにすることは禁止しない。今回の完了例外を恒久化せず、旧schema migration/alias/fallback/別DBを追加しない。

### 追加回帰条件

1. 現canonical Taskへquality Validationを保存し、同じidentityで取得/clear/saveできる。別Task runや変更後snapshotを拒否する。
2. real dispatchとdedupを使ったfinalize-checkがPASSへ進み、拒否/不整合ではgateを走らせない。
3. stop/quota resumeとvolatile ref add/update/deleteが通常のsnapshot admissionを通り、実authority変更は安全停止する。成功した5h復帰とその後の停止を混同しない。
4. accepted seal後のHandoffでPublicationとterminal retirementが合法になり、通常push refspecがGuardを通る。未知/無権限pushは拒否する。
5. 同じ実装を分割commitせず、normal Review/Validation/accept/Publication/retireを完遂する。clean indexと実Git/remote/controller一致を確認する。
6. full Go/lint/vet/build/install-smoke/独立reviewを新しい実変更に対して実施する。今回のBundleや旧PASSを次Taskの検証結果へ転用しない。

## Purpose

通常glm-worker workflowで成功する境界と不整合停止する境界の条件差を一次証拠から特定し、実装の回帰であれば正規ownerの責務内で修正する。必要な正規lifecycle経路が実際に不足している場合は、その不足も同時に解消する。同じTaskの実装修正は1 commitにまとめ、次の作業を一時operatorやState捏造なしで継続できる状態にする。

## External feasibility

status: not-applicable

## Contract

- 実装開始前にcurrent Git、導入前後のGit/Issue/PR、既存testsとcontroller/runner/workflowの正規contractを調査する。未確定の原因や比較条件を推測で設計しない。
- 同じBundle Taskの成功・失敗ケースを比較する。quota停止前、停止checkpoint、quota回復直後、worker終了、quality gate/lint/format/full Go終了、auto-fix/review入口の各境界で、HEAD/index/worktree/ref digest、snapshot ID、generation、lease/attempt/model-call identity、mutation owner/recordの対応を復元する。
- 5h limitからの自己resumeが通ったケースと、resumeで止まったケース、resume後に別境界で止まったケースを区別する。成功したquota復帰を後続停止へ付け替えず、実装変更・Git変更・親metadata変更・volatile refs・gateによる変更のどれが原因かは確認した証拠で判断する。
- controller authority、runnerの既存volatile-ref境界、worker変更権限、machine gateの変更責務、親metadata/read-only境界の不一致を調べる。scopeを問わず比較を緩和する修正や任意snapshot採用を先に選ばない。
- データだけの不整合か、通常経路で再発するソースコードの不具合かを判定する。ソース不具合を確認した場合だけ、root causeと修正semantic boundaryをSolへ提示して正規ownerで修正する。既存contractに照らしたfalse positiveの場合は不要な修正をしない。
- 調査・修正・独立review・validationはGLMへ委譲し、親は意味判断とmetadataを所有する。過去の長大調査を無条件に反復せず、未確定点と対照ケースに限定する。
- 最新Amendmentの限定承認による起動ownerの直接修正も、本Task全体のGLM検証・独立Review・最終1実装commitの対象とする。正規execution laneへの引継ぎではprimary worktree（`/Users/shinderumanm/src/codex-worker-orchestrator`）に保持された同一Taskの未commitソース・テスト差分（引継ぎpatch: `/private/tmp/normal-workflow-startup.patch`、primaryの実物を正とする）を読み、既存のmutation ownerで実行laneへ反映する。bootstrap・Handoff・Project authority同期と正規GLM Task要求の成立後は親Codexによる直接実装を終了する。
- 不整合停止が妨げている親metadata登録・candidate生成・accept/seal・publish・Task retirement・次Task admissionのうち、該当する既存ownerと履歴・testsを調べる。同じTaskの実装修正を1 commitへまとめられる既存経路は維持し、Plan/Task終了metadataを別commitにすることを理由にpublicationを再設計しない。必要な正規経路の不足が確認された場合は、責務・authority・公開API・永続状態の意味をSolへ提示してから実装する。
- 親によるPlan/Task定義の追加・修正はTask実行とは独立したmetadata操作として扱う。Task開始前やTaskが実行されていない時にも行えることを維持し、1 Task＝1 commitの制約をmetadata操作へ拡張しない。metadata編集を行うためだけにTaskを開始したり、登録したNEXTを実行開始扱いしたりしない。既存canonical authority・親専有境界は維持する。
- commitをまとめるためにreview・validation・install・sealed evidence・remote OID確認・Task終了のpostconditionを省略しない。親metadataを先にTask終了へ変更することで未完了を完了扱いする経路を作らない。GLMによる親metadata編集やGit remote writeを許可しない。
- Bundleの未完了publication/retirementに関する旧記述は作成時点のhistorical contextである。最新Amendmentに従いGitとControllerの現物で残存不整合を判定し、保存済み成果と一次証拠を保全する。前TaskのValidationを本Taskへ転用しない。

## Must not

- 要求定義だけを準備するという旧制限は最新Amendmentで上書きする。本Taskを正規Workflowの復旧まで実施し、保存済みBundle evidenceを保全する。Bundle exporter本体やCLI admissionの別責務の実装を本Taskへ混ぜない。
- failed-closed状態を通すことだけを目的に、恒久的なbootstrap exception、汎用recovery/fallback/互換command、第二authority/state DBを追加しない。
- 今回限りのState修復、checkpoint捏造、一時binary、check無効化を本Taskの正規成功条件にしない。generation rollback、state削除、head直接上書き、証拠の再帰属やPASS捏造を行わない。
- actual mutation/task executionのlocking、parent metadata不変性、未知のGit/source変更に対するfail-closedを弱めない。
- userが書き換えたGit/Remote履歴を再書換えしない。GLMにcommit/pushやparent metadata編集を許可しない。
- 原因未確認のまま過去のcontroller recovery patchを清書・再install・恒久化しない。

## Acceptance criteria

1. 停止するケースとしないケースの対照、quota復帰の成否と後続停止の区別、各snapshot/lease/mutationの因果をexact証拠で説明できる。
2. データ不整合・ソース不具合・観測/操作の誤りを区別し、ソース修正が必要なら根本原因と採用した正規semantic boundaryを説明できる。
3. 実装回帰を確認した場合、通常worker変更→gate→auto-fix/review、およびstop/quota resumeを正規コマンドで実行し、一時operator/State修復なしで同じTaskのlifecycleを進められる。
4. 必要なregression testsで正規変更の受理と非正規source/Git/metadata変更の安全停止を両方保証する。失敗・成功の実データから得た条件差をtestに含める。
5. repository lint、必要なfull Go tests、vet、build、install-smoke、独立review、通常配置後のproduction smokeを通す。未検証を成功扱いしない。
6. 同じTaskの実装修正をファイル別など複数commitへ分割せず、1 commitにまとめて正規公開できる。Plan/Task終了metadataは別commitでよい。履歴書き換えを必要とせず、レビュー・validation・install・controller evidenceのcanonical bindingとremote OID一致を保持する。既存経路で成立する場合、これだけを目的にソースを変更しない。
7. 正規publication/lifecycle経路を変更する場合は、変更境界に応じた失敗・中断・retryのregression coverageを用意する。実際の公開状態とcontroller/Task状態を区別して保持し、同じ実装修正の重複commit・未公開状態の完了扱い・未許可NEXT開始を防ぐ。
8. Task終了後、通常配置したglm-worker/glm-parent-actionで次Taskの正規開始が可能な状態を確認する。確認だけを理由に無関係なNEXT Taskの実装を開始しない。

9. Retire後・次Task開始前の正常状態と親metadataだけの正当な更新を既存ownerで扱い、同じ状態で追加のユーザー介入なしにbootstrap、Handoff、Project authority同期、通常Task開始を成立させる。任意の外部Git変更は拒否し、旧terminal record・Controller履歴・証拠を保持する。

## Review findings

- laneの所属はcookie `controller-workspace.json`とmaterialize transition `330c395e-07d0-48c7-bf69-b1d2a01b3e5a`が示すworkspace `46285ae1-2a92-4dba-8f0a-391bb4b304c4`、Attempt `00ddd594-ad9d-40a1-895f-f49106384457`である。下記32/9件は最新terminalのprimary workspace sealとの比較であり、lane自身の未保存データを意味しない。lane自身のimmutable sealは`evidence/objects/7c/7cfb70ec8c9fe838f7bb55060584353b96282a6bf962015a50307e3e8eeb4f65`、archiveは`evidence/objects/33/33d4452f3525fd850d9e3076463087187780cccc096f2c738a121e03d1352bf8`。lane自身のsealでは親metadata外のindex/worktree差は0で全blobが保全済み。staged 0件/unstaged 32件を別treeで保持する。実際の親Task Markdown 1件は正規化済みtreeと異なるため、cleanup ownerが正規化前の実index/worktreeを復元可能なportable archiveへ保存し、cleanup直前に照合する。元sealの再生成や内容一致Guardの緩和をしない。
- accept transition `56722ec6-92ac-46b7-966b-885bcdda56db`（generation 388→391）以後、laneのcleanup/materializeを経ずprimary workspaceの後続Attempt `17bb0914-b8d8-4146-a1bb-adacf2c0b809`、`00a28254-7940-4785-9c9b-b6761a23ca50`へ移っている。各Attemptのimmutable sealのPredecessorAttemptIDとResumedFromSealIDでlaneを辿る。これは現在の通常materializeがprimaryへ切り替えた証拠ではない。通常のRetire後にも開始ownerがsealed lane cleanupを呼ばず占有拒否する不足を別に扱う。直接修正部分の保全・cleanup・再利用もGLMによる独立Review/Validationの対象とする。

- 正規の次Task開始前に、退役済みBundle Taskのcanonical laneが残存している。既存materializeは`execution lane is occupied`で停止する。対象は`~/.glm-worker/controllers/5156de56d87a719a79cdda90e93a63aebfc6e562d085eea527b652f058d0e3e2/5156de56d87a719a-lane`。terminal record `be522186-324b-4f01-8a37-1238eb089f42`、Attempt `00a28254-7940-4785-9c9b-b6761a23ca50`、workspace `8078b3f128bcb12fd14d7610005b6cad85bd29861076875eb02409274df6e5ca`に対応するsealはdigest `c05b41b7c6d07321f34b679068ce2253a78a5f8fc6d1f171cdf566386bee0378`（controller evidence object `evidence/objects/c0/c05b41b7c6d07321f34b679068ce2253a78a5f8fc6d1f171cdf566386bee0378`）。sealは現在のevidence graphから到達可能で、既存cleanup-durability ownerの検証は成功した。ただしlaneの実indexはsealのCurrentIndexTreeと親metadata外で32 path、実worktreeはCurrentWorktreeTreeと9 pathで異なる。同じ9 pathは公開済み`8b7556b`とも異なるため、公開済み成果と同一だとして削除しない。実index/worktreeを保持したlane、immutable seal、および公開Git treeを一次資料とする。差の原因は未確定であり、過去のState操作の影響とコード回帰を混同しない。
- 上記worktreeの9 path: `glm-worker/internal/app/controller_evidence_export_test.go`、`glm-worker/internal/controller/evidence_export.go`、`glm-worker/internal/controller/evidence_export_attempt.go`、`glm-worker/internal/controller/evidence_export_live.go`、`glm-worker/internal/controller/evidence_export_test.go`、`glm-worker/internal/controller/evidence_transcript_window.go`、`glm-worker/internal/controller/runtime_evidence.go`、`glm-worker/internal/controller/runtime_evidence_sessions.go`、`glm-worker/internal/controller/runtime_evidence_test.go`。sealへの内容一致条件を緩和せず、旧実データ・旧terminal record・履歴・証拠を保全した正規再利用を既存owner内で扱う必要がある。未承認の自動cleanup draftを配置済み・検証済みと扱わない。

## Historical invariants

- single canonical repository controller authorityを維持する。
- Codex ReductionとSol Highの意味判断を維持し、原調査・実装・検証をGLMへ委譲する。
- 一時運用例外と通常経路のソース修正を混同しない。

## Dependencies

none
