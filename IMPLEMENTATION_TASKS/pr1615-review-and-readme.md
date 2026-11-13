# PR 1615の指摘対応とREADME見直し

## Original instruction

TASK

https://github.com/shinderuman/codex-worker-orchestrator/pull/1615

REVIEW_RANGE:
`ca0d2e9c83124d0335ffcd20b959d48b1ac66c70...4715808beb66ef2efd7aa157c8271cfb1244e577`

* この依頼専用のACTIVE Taskを1件作成し、そのTaskだけを完了させる。
* Codex自身が全指摘の原文と現行実装を確認し、採否と修正方針を判断する。GLMへ判断を丸投げしない。
* 既対応・誤検出は除外する。既存Taskと重複する指摘は、そのTaskの要求を必要に応じて補完する。
* それ以外の有効な指摘は、すべて現在のACTIVE Task内で実装・検証まで完遂する。明らかに大規模な修正が必要な場合に限り、Codexの判断で独立Task化を認める。

- 必要に応じてGit履歴・差分や周辺実装を先に分析し、問題の発生原因、修正すべき箇所、優先順位を検討する。特に退行修正では、現在の症状だけでなく、退行を引き起こした変更を調べる。
- その判断を踏まえてGLMに具体的な調査・実装・検証指示を出す。作業内容を把握せず丸投げしない一方、GLMが担当すべき詳細調査や実装まで親Codexが重複して行わない。分析の深さはTaskの性質とリスクに応じて判断する。

# 親Codexによる実装成果の確認と最終採否判断

GLMは調査・実装の担当であり、作業結果の最終的な採否とTask完了の判断は親Codexが行う。

GLMの作業報告、KEEP判断、テスト結果の要約、「対応完了」という宣言だけを根拠に、要求を満たしたと判断しないこと。

ただし、親CodexがGLMの作業を全面的に再調査・再実装・再Reviewすることも目的ではない。親Codexによる確認は、GLMへの委譲によるCodex消費削減という利点を維持しながら、重大な見落としや未完了を防ぐためのものである。

## 実際の差分の確認

- GLMの報告を確認の入口として利用し、Gitの実差分、変更ファイル、必要な周辺実装、検証証拠と照合する。
- 変更が要求とAcceptance criteriaに対応しているか、必要な変更が欠落していないか、対象外の変更が混入していないかを確認する。
- 特に重要な責務境界、不変条件、削除・置換した機構、残存する旧処理、失敗・復旧経路については、報告だけに依存せず実コードを確認する。
- 削除・整理を指示した場合は、実際の差分と残存実装を照合し、一部だけ変更して完了と報告された状態を見逃さない。
- 既存機構の維持をGLMが提案した場合も、その必要性を要求・依存関係・不変条件から判断する。既存実装や関連テストの存在自体を維持理由としない。
- GLMが実施した検証の対象、結果、source bindingを確認し、確認されていない事項を成功済みとして扱わない。

## 確認コストと修正の制御

- すべての変更行を無条件に精読するのではなく、変更規模、失敗時の影響、責務の重要性、GLMの報告と実差分の不一致に応じて確認範囲を決める。
- 既存の独立Review、検証結果、Git証拠を利用し、同じ確認や高コストなテストを理由なく繰り返さない。
- 問題が確認された場合は、具体的な不備、該当箇所、満たすべき条件を明示してGLMへ修正を依頼する。
- 修正後は指摘事項とその影響範囲を中心に確認する。毎回全体を最初から再Reviewしない。
- 客観的な要求違反、重要な設計問題、品質上の欠陥がない限り、Codexの主観的な好みや過剰な完成度追求による修正往復を発生させない。
- 未達事項が残る場合は未完了として扱い、何が不足しているかを明示する。確認コストを抑えるために未達を完了扱いすることもしない。

## 最終判断

CodexはGLMの結論を追認するのではなく、要求、実差分、重要な不変条件、独立Review、検証証拠を突き合わせて最終採否を判断する。

その際、確認の厳密さだけを最大化せず、品質維持とCodex消費削減の両方を考慮する。

**目標はCodex自身がすべての作業を理解し直すことではなく、重要な不備を見逃さず、必要十分な確認でTaskを完了させることである。**

GLMへの委譲によって節約したCodexのトークンを、親Codexの重複調査や際限のない修正往復で消費しないこと。

意味のある状態遷移・完了・エラー・ユーザー入力がない限りparent modelへ戻らず、blocking waitには `yield_time_ms: 21600000` を使用してください。

`30000`、`60000`、`300000` 等への短縮、経過時間・無変化・liveness確認を理由とするpollやparent model再入は禁止します。不要なparent model再入はCodex tokenを消費し、このツールの最上位目的であるCodex Reductionに反します。

あとこのタスクで合わせてREADMEの見直しをしろ

> `install.sh`と同じ`GLM_WORKER_BIN_DIR`、`CODEX_CONFIG_DIR` / `CODEX_HOME`、`CLAUDE_CONFIG_DIR` / `CLAUDE_SETTINGS_FILE`で撤去先を指定できます。既存の所有権記録で確認できたCLIバイナリ、Codex管理ファイルと管理設定キー、Claude管理設定と環境設定、repository固有のGit hooksのみ撤去します。別途導入したquality tools、ユーザーのCodex/Claude設定、credentials、sessions、telemetry、bundleは保持します。管理対象が外部変更されていた場合は撤去を拒否してエラーとし、修復後に再実行できます。同一OSユーザーでCLI、設定、hooksを変更する段階は排他されます。競合した操作は所有物を変更する前にエラーになります。ビルドなど変更前の準備は並行できます。排他に使う一時ディレクトリと保護ファイルは、インストールやアンインストールの実行中は削除しないでください。アンインストールに通常のinstall/buildやnetwork downloadは実行しませんが、ローカルGo toolchainと既存module cacheを使用します。

とかどう考えてもREADMEに乗せる情報量じゃない

クソだ

全体を削れと言っているわけではない

見直しをしろ

お前はクソ化するプロだからクソな状態を見直せ

記載すべき内容が記載されてない状態になってないかも考えろ

だが冗長なクソ化はするな

これは矛盾する話ではない

## Amendments

### 2026-11-13 ユーザー追加指示

1タスク1コミットで作業するならやっていい
登録だけのコミットを作るなということだ

### 2026-11-13 空ACTIVEからの口頭依頼開始

いやそれじゃ今後このリポジトリ、タスクがない修正はできないってことじゃん
今回のように直接口頭で伝えて作業させることは当然あり得る
今回の修正で合わせて修正しろ

### 2026-11-13 GLM Limit後の直接引継ぎ

GLMがLimitになったからあとの作業はお前が引き継げ

### 2026-11-13 Publish前の外部レビュー

ちょっとやり方を変える
今回外部レビューをもとに作業をしたがPublishする前にも外部レビューを行う
なのでPublish前にPRを作れ
ただしCodeRabbitやGreptileの呼び出しは禁止だ
お前がPRを作ったタイミングで俺が呼び出す
そして指摘があり対応が終わったら次の外部レビューも俺が呼び出す
すべてが問題なくなったらPRをMergeするのではなくPublishを行う、つまりPRは破棄する
そういう形で進めろ

### 2026-11-13 1コミット制約の適用時点

1コミットを強調しているがPublish前に1コミットにすればいいのでこのあとPR対応をAmendしたりする必要はない当たり前だが

## Resolved references

- 対象PR: https://github.com/shinderuman/codex-worker-orchestrator/pull/1615
- 対象range: `ca0d2e9c83124d0335ffcd20b959d48b1ac66c70...4715808beb66ef2efd7aa157c8271cfb1244e577`
- 親が全指摘原文・現物を確認して採否を確定する。採否は実装前に本Taskへ追記する。

### 親Codexの採否・修正方針（2026-11-13）

- 採用: `discussion_r4560061819`と補足`4560120721`。現行`admission.go:bootstrapWithoutLiveLease`はzero Task同士のEqualを通し、`bootstrapFreshExecution`が記録を永続化する。range内の`committed_task.go`がidleのzero Task解決を許した変更が発生契機。idle schedule自体の有効性は保ち、Runtime bootstrapだけを永続化前に明示拒否する。RequestIDのあるrequest-backed Taskを空扱いしない。正規app入口・直接bootstrapからhead/attempt/leaseが汚染されない回帰を検証する。
- 不採用: `discussion_r4560061824`。`task_authority_sync.go:verifyParentOnlyTaskSourceDelta`の欠損拒否はcurrent-only契約に整合し、旧leaseだけを理由とするmigration要求は採用しない。`4560123323`による不採用理由と`4560128109`の撤回も確認した。現行正規フローで欠損する別証拠は提示されていない。
- 採用: `discussion_r4560062552`。現行`projectCandidateSourceAdvancement`はprepared/promotedを対象にする一方、追加されたremote観測helperがpromotedかつEvidenceValidだけに限定する。preparedまたは再検証中のremote-only advancementでも正規rebindを投影・実行できるよう整合させる。既存evidence無効化・source再束縛の条件を弱めない。状態×evidence validityを回帰で検証する。
- 採用: `discussion_r4560062557`。remoteがcandidate commitまたは子孫のときhelperはfalseを返し、promotedのpublishが投影されるが`terminalSourceProject`はremote!=baseを拒否し、rebindも包含を拒否する。remote包含を正規に観測し、immutableな公開lineageを保持した最終化/adoptionへ到達させる。未検証candidateを公開済みという理由だけで成功扱いしない。新しい公開operation/state意味変更が必要なら実装前に設計案・現行経路・拒否条件をSolへ返す。
- 採用: `discussion_r4560093773`。range内`advancingZoneSegmentEnd`が非前進境界で1秒加算へ置換されたため、cursorからlimitまで残秒数に比例する走査になる。異常なZoneBounds領域を単調かつ有界に進める。DST・日跨ぎ・年末・exclusive endのinterval意味を保ち、wall-clock依存でなく走査回数を確認する回帰を含める。offset変化を飛ばす疑義のある方式は実装前に根拠と選択案をSolへ返す。
- README: 親が全本文を確認した。撤去節は管理内部仕様を過密に列挙している。導入前提・導入手順・基本利用・設定の入口・撤去方法と利用者データ保持・開発規則へのリンクを必要十分な日本語で案内する。詳細が利用者の操作に必要なら既存docsへの整理を優先し、単に長文を移して第二正本を増やさない。新規利用者がprovider設定と対象repositoryでの有効化方法へ到達できるかを現物で確認する。
- 現在の既存BLOCKED Task群には同一指摘の要求を見つけていない。後続Taskを開始・完了せず、上記をこのTaskで扱う。
- 1タスク1コミットの開始に関する追加一次証拠: `app/execution.go:activateNewWorkflowConfig`は、既存controller・live leaseなし・ExecutionTaskRefなしで`MaterializeExecution`へ進む。idle完了headはRootTaskRefもnilであり、`controller/execution_lane.go:validateMaterializationHead`はnil rootを拒否する。未コミットの新ACTIVE Task登録だけではこのheadに新task authorityが束縛されず、正規開始が`materialization requires exact quiescent controller authority`で停止する。登録だけのコミットやcontroller state直編集で回避しない。開始ownerの変更が必要な場合も、このTaskの1コミット境界を維持する。
- 開始ownerへの親の直接修正方針: current worktreeのTask/Planを既存parent-source revisionとして捕捉し、pristineまたはfinalized idleと一致するprimary/remoteからのみ実行authorityを開始する。公開refや履歴へ登録commitを追加しない。現行live Task・candidate・pending transition・external advancementを置き換えない。worker/reviewerは開始修正も本Task全体のdiffとして検証すること。特にsource revisionのarchive/provenanceが復旧・GC・最終Publicationまで保持されるか、登録Taskと実装を含む1コミットでretirementできるかを確認し、欠落があれば既存owner内で補う。

### PR 1617の外部レビュー採否（2026-11-13）

- レビュー対象: https://github.com/shinderuman/codex-worker-orchestrator/pull/1617 、HEAD `5d191f03164fd215f595ec2a4f370d3ea5f8a9f6`。
- 採用: `discussion_r4561402662`。`StartRegisteredTask`がparent-only指定を拒否する一方、既存parent-only admissionはcommitted Taskを要求する。登録sourceを既存parent-only materializationへ接続し、Runtime/GLM開始との責務分離を維持する。pristine idle・完了後idleの双方で登録専用コミットなしに開始・完了できることを検証する。
- 採用: `discussion_r4561415245`。ObserveOnlyのremote非包含時はprepare済みtransitionからerrorだけを返しpendingが残る。非pushを維持し、既存のcompletion abortへ収束して次の正規操作を投影できるようにする。remoteがbaseへ戻った場合と別子孫へ進んだ場合、recovery反復でもpendingが永久に残らないことを検証する。
- 採用・重複統合: `discussion_r4561384221`、`4561430406`、`4561439515`。登録source復元がLiveAttemptIDだけに依存するため、candidate受理後のsealed attemptからも同じsource archiveを復元する。受理後・promote後にGCしてpromote/publishまたはadoptまで完遂する回帰を検証する。
- 採用: `discussion_r4561430390`。APIキーをliteralなexport commandへ埋める例はshell履歴へキーが残る。履歴へ値を記録しない入力方法に変更し、README補足を冗長化しない。
- 対象外: `discussion_r4561368646`のdescendant親管理Markdownによるcompletion拒否要求は撤回済み。candidate自身の完了と後続descendant adoptionの責務分離を維持する。
- 対象外: `discussion_r4561430403`のmaterialization退行説は、RootTaskRefありの正常後続Taskが新分岐へ入らず、nil rootを旧materializationも拒否するため不成立。`4561469411`の反証と`4561481322`の撤回を確認した。
- 対象外: `discussion_r4561430412`のlocal descendant許容提案は、exact candidate treeを要求するindex/worktree保護と矛盾する。`4561469848`の反証と`4561476181`の撤回を確認した。local equalityを弱めない。

### PR 1617の追加外部レビュー採否（2026-11-13）

- レビュー対象HEAD: `41c2cede908b683977ef506143eefb5459d0234c`。
- 採用: `discussion_r4561915444`。Task同期後のProjectSnapshotは新しいsynthetic sourceを指す一方、復元が開始時のAttempt.SourceRevisionへ依存している。開始時のsourceはparent-onlyの基準として維持し、synthetic sourceを作る各ownerが対応するarchiveをProjectSnapshotへ束縛する。復元時はarchiveのcommit rootとsnapshot.HeadOIDの一致を検証し、snapshot自体のHeadOIDによる意味的identityは変えない。live/受理後/昇格後の復元をAttemptの状態に依存させない。
- 関連する実装上の不足: 登録Taskの同期は物理HEADをtree基準にすると未コミットのPlan登録を失う。同期元のProjectSnapshotをtree基準とし、登録時のprimary sourceからTaskだけが変更されたことを検証する。Task以外の未承認変更を許容しない。登録→Task改訂sync→GC→受理→昇格→公開をRuntime/parent-only双方で確認し、登録専用commitを作らず成果物を保持する。

## Purpose

指定PRの有効な指摘を修正し、READMEを利用者向け入口として必要十分な情報へ整理する。

## External feasibility

status: not-applicable

## Contract

- この依頼専用Taskで、親が指摘を採否判断した後、GLMが必要な詳細調査・実装・検証・自己reviewを担当する。
- README全体の構成と情報の過不足を確認し、導入・基本利用・設定・撤去の入口を維持しつつ、実装詳細や過密な運用説明を整理する。
- 実装とTask終了metadataを1コミットへまとめ、登録だけのコミットを作らない。
- idle完了でACTIVEが空でも、口頭依頼を親が専用Taskへ固定して通常のGLM workflowを開始できるよう、開始ownerを修正する。未コミットTaskを理由に登録専用コミットを要求しない。今回の開始障害を復旧する範囲は親の直接修正・検証許可として扱い、それ以外はGLMへ委譲する。
- 最新Amendmentにより、GLM Limit後の残る本Task実装・検証は親Codexが直接引き継ぐ。GLMへの委譲を要求する従前Contractは、この残作業について直接実装許可で上書きする。
- Publish前にレビュー用PRを作成する。CodeRabbit/Greptileへの呼出しやメンションは行わず、ユーザーの外部レビュー呼出しと結果を待つ。指摘へ対応した後の次レビューもユーザーが呼び出す。全問題解消後もPRをMergeせず、正規Publishを行いレビュー用PRを閉じる。このレビュー待ち境界ではTaskを完了扱いしない。
- 1タスク1コミットの制約はPublish直前の最終成果へ適用する。レビュー用PRの指摘対応は通常の追加コミットで進め、都度のAmendや履歴書換えを要求しない。Publish前に実装と終了metadataを1コミットへまとめる。登録だけのコミットは作らない。

## Must not

- 既対応・誤検出・撤回済み指摘を根拠なく実装対象に戻さない。
- 機能追加、無関係な改善、READMEの一律削減、必要な利用案内の欠落を行わない。
- GLMへ最終採否を丸投げしない。親も低レベル調査・検証を全面的に重複しない。
- GLMにcommit/pushやparent-managed metadata編集を許可しない。
- 他の既存Taskを実装・完了しない。
- CodeRabbit/Greptileを呼び出さない。レビュー用PRをMergeしない。外部レビュー前にPublishしない。

## Acceptance criteria

- 全指摘の原文、現行実装、必要な退行差分に基づく親の採否と修正方針が確定している。
- 採用指摘に対応する実装・回帰検証があり、独立Reviewと親の重要差分確認を通る。
- READMEの冗長な詳細を整理し、初見利用者が導入・利用・撤去と詳細情報へ到達できる。
- 実差分と検証source bindingを親が確認し、必要なquality gate・install・smokeを満たす。
- このTaskだけを完了し、1タスク1コミットで終了metadataを同期する。
- pristine idleと最終Task完了後のidleの双方から、新規口頭依頼を有効なTask authorityへ束縛して開始できる。要求なしの空Taskからlive executionを生成しない。Task登録から実装・退役まで1コミット境界を保つ。

## Historical invariants

- machine-only stateはcurrent-only、古いleaseの存在だけを理由とするmigrationを導入しない。
- publicationの証拠整合・remote観測・Task/lease境界を弱めない。

## Dependencies

なし。
