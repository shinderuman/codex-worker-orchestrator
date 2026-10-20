# Task: Workflow全体の検証コスト削減とapp責務境界の整理

## Original instruction

````text
対象repository:
/Users/shinderumanm/src/codex-worker-orchestrator

目的は、品質保証と最終source bindingを維持しながら、開発開始から検証・Publication・Retire・Cleanupまでの総時間とCodex消費を削減し、責務が集中したinternal/appの構造を改善すること。

速度改善に重きを置く。ただし、internal/appの責務集中そのものも設計上の改善対象とし、必要な分割の採否を即時の実行時間短縮だけに限定しない。責務の明確さ、依存方向、変更影響、理解・検証・保守の容易さも評価する。

開始時点のコード・CI・設定・運用規則を現物で確認し、それらを前提に作業する。既存の変更や未コミット差分を保全し、古い構成へ戻さない。

依頼を既存Planと照合してtracked化し、重複する要求は統合する。今回実施するACTIVEを一件に定め、対象外のTaskには着手しない。ACTIVE完了後は次Taskへ進まず停止する。

評価・実装対象:

1. finalize-checkから完了処理までの重複
- full suite、repository lint、source確認、validation照合、candidate/evidence/archive検証、履歴閉包収集について、目的・owner・実行回数・所要時間・実効入力・失効条件を整理する。
- 同じ最終commitに対するlint等の重複を、単一ownerへの統合・配置変更・既存証拠の安全な利用で除去できるか比較する。
- 安価な早期検証を高コストなfull suiteより先に配置する効果も評価する。
- 内容アドレス付き不変データの検証と、source・最新validation・remote・lease等の変化し得る境界検査を区別する。
- 必要な境界検査を維持し、不要な再走査・再構築・状態遷移・証拠の複製を削除または統合する。

2. internal/appの責務集中
- appに集中している実装・testを、責務と依存関係から評価する。
- appを入力処理、依存の組み立て、dispatch、出力などのCLI入口と機能の接続に絞る。
- 機能固有の判断・実行・データ変換は、責務を持つ既存packageへ移すか、必要なpackage分割を行う。既存packageの境界も必要に応じて見直す。
- 実装の移動とともにtestの責務・配置・fixtureを整理し、単体testとCLI統合testを適切なownerへ置く。
- appへの逆依存、循環依存、薄いwrapperの増殖、テスト移動のためだけの公開API、過剰なinterface・共通基盤を避ける。
- package数・ファイル数・行数を削減目標にせず、責務と変更影響が整理された構造を成果とする。

3. package構造とテスト実行コスト
- package分割による並列実行・変更影響・compile/test cacheへの効果を評価する。
- 全repository fixture作成、外部process、lint、Git/archive処理自体の重さと、package内で直列化している時間を分けて調べる。
- 責務分割と、高コスト処理・fixtureの重複除去を組み合わせて検討する。
- 同じpackage内のファイル分割やtimeoutの分散だけを高速化成果にしない。
- 並列化によるメモリ・I/O負荷や競合も確認し、wall-clockだけを見て採用しない。
- 効果と構造上の必要性を確認できる責務から変更し、大規模な一括移動を目的にしない。

判断基準:
- 重複処理の削除・統合、検証配置変更、責務分割、fixture改善を、Workflow全体のwall-clock・Codex消費・メモリ/I/O負荷・検証保証・保守費用で比較する。
- 現在存在することや関連testがあることだけを機構の維持理由にしない。機構自体を削除・統合できないか確認し、不要になるGuard・State・Fixture・Testも整理する。
- 独立した重要な故障モードの検出能力は維持する。
- 新しいcache・State・管理機構の追加を前提にせず、既存処理を削除・統合する案と比較する。
- 既存の調査・ログ・計測を優先し、追加計測は判断に不足する箇所へ限定する。
- 通常成功と、検証FAIL後のFix・再Review・再公開について、変更前後の実行回数・時間・source binding・維持する保証を示す。
- 環境条件やCI変更による差と、実装による効果を区別する。単一runの時間差だけで高速化を断定しない。
- 委譲先のKEEP判断や完了報告をそのまま採用せず、親Codexが重要な責務境界・不変条件・実差分・残存機構を確認して最終採否を判断する。

維持する条件:
- 古いsnapshotのPASSを流用せず、必要な検証を省略しない。
- timeout延長だけで解決扱いにしない。
- 現在候補の厳格な採否検証と、歴史archiveの参照保持を混同しない。
- 専用操作や手作業のState補正を通常経路へ組み込まない。
- 通常成功・Fix/再Reviewの成立は、State補正なしの正規入口で検証する。
- fixtureや代替processを使う接続検証と、実suite・実運用の成功証拠を区別する。
- 必要な独立Review、変更に対応する検証、最終sourceの品質ゲート、本配置・Publication・Retireを正規経路で成立させる。
- 調査目的だけで高コストなfull suiteを反復しない。修正後に必要となる最終検証は成立させる。

実装した変更、構造改善の理由、時間・Codex消費への効果、維持した保証、測定条件と未検証事項を示して完了する。
````

## Amendments

### 2026-10-21 GLM主体の作業（ユーザー原文）

````text
Codexの直接修正は最低限にしろよ
作業のメインはGLMでやらないとトークンが無駄だろ
````

### 2026-10-21 外部修正後の起動阻害と最短復旧（ユーザー原文のsemantic delta）

````text
だがいま外部で修正、例えば俺が直接コードを弄ったりすると正常に作業を開始できない状態にあると思っている

それも合わせて作業しろ

まずは最短経路でGLMまでのルートを修正しろ

最初の最短経路に限ってはStateを直接修正することも許可する
````

### 2026-10-21 起動阻害を今回だけの迂回にしない（ユーザー原文）

````text
これ、他で作業してそれをPullしたら作業できないってこと？それなら致命的にバグだから次のセッションで合わせて不具合修正をしてほしいんだが
単純に今回だけ迂回とはしてほしくない
次セッションが勘違いしているだけなら普通に作業させてほしい
````

### 2026-10-21 既存archive費用評価要求の統合

既存の同一評価対象を本Taskへ統合し、独立Taskとして重複実行しない。統合元の要求本文を以下に保存する。要求統合は元Taskの実装・検証成功を意味しない。

`````markdown
# Task: controller内部Git archive費用評価

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
内部durable Git archiveでrootごとに全reachable履歴をpackする費用を測定し、証拠の耐久性と品質を維持できる改善だけを採否判断する。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の C1。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

内部durable Git archiveでrootごとに全reachable履歴をpackする費用を測定し、証拠の耐久性と品質を維持できる改善だけを採否判断する。

## External feasibility

status: not-applicable

## Contract

- baseline/candidate/accepted seal/terminal各取得のtime、pack/envelope bytes、共通object、disk retentionを代表履歴で測定する。parent再入・再調査と実Codex消費への寄与は、測れた範囲だけ報告する。
- portable Bundle exportと内部durable archiveのownerを区別し、Bundle修正済みという理由で内部重複の費用を解消済みとしない。
- MEASURE-FIRSTとし、効果が小さい場合のno-changeを正規完了にする。改善案に永続化意味・cleanup graphの変更が必要なら、採用前に親が具体設計を確定する。
- 有意な改善を採用する場合もcurrent canonical evidence storeとseal graphを正とし、offline復元、検証、cleanup後の到達性を維持する最小変更にする。

## Must not

- object重複観測だけでCodex削減率や全体bottleneckを断定しない。
- 第二store、旧schema migration、無条件history切捨て、検証省略を先行実装しない。
- 長期archiveや新daemonの増設を評価の既定解にしない。

## Acceptance criteria

- 小規模履歴、大blob削除済み履歴、複数seal/rootの比較で費用内訳を再現可能に示す。
- 採用/no-changeを測定値と品質条件で親が判断する。削減不立証もそのまま記録する。
- 採用時はcleanup後のoffline復元・seal検証・missing object拒否のregressionを検証し、関連quality gateを通す。no-change時も評価の入力・方法・限界が回収可能である。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
`````

## Resolved references

- 外部修正後の起動阻害には、正常完了・Retire後のclean primaryへコード/CIの外部fast-forwardを取り込むとbootstrapと正規adoptがmetadata専用guardで拒否される実例がある。Pull全ケースに一般化しない。
- 起動阻害の恒久修正は最新指示により本Taskへ統合する。初回の最短復旧に限る直接修正・State補正の許可を、通常Workflowの救済操作や恒久的なguard回避へ拡張しない。復旧後の通常調査・実装・test・lint/buildはGLMへ戻す。
- 原要求のACTIVE完了後停止は本Taskの実行境界であり、無関係なNEXTを開始しない。

## Purpose

品質と最終source bindingを維持しながらWorkflow総時間とCodex消費を削減し、appへの責務集中を改善する。

## External feasibility

status: not-applicable

## Contract

- 本題の開始阻害として外部修正後の正常開始を恒久修復する。read-only authority取得と明示的source同期mutationを区別し、既存ownerでclean primary・HEAD/local/remote・fast-forward・committed Task authority・完了済みquiescenceを検査する。metadata専用guardを無制限source許可へ拡張しない。
- dirty、divergence、remote不一致、live lease/attempt、未完了候補/Task/Runtime、pending transitionの衝突を区別し、既存作業・証拠を保全する。完了後に選ぶ未着手successorのmetadata同期は旧Runtime完了とtarget Task/Project一致を検査する。
- 正常完了→外部コード/CI修正→Pull→read-only bootstrap→正規staged同期→次Task開始を、歴史のあるControllerとState補正なしの通常経路で回帰確認する。初回の直接復旧をこの成功証拠へ転用しない。
- 同期のprepare/apply間race、追加fast-forward後の旧journal/terminal履歴検証、RequireInstall時の旧install PASS非転用、metadata-only successor切替と旧Runtime未完了拒否を確認する。source同期は新sourceの品質PASSや本配置完了を意味せず、必要な新candidateのreview/validation/installはfreshに成立させる。
- 親が起動復旧で準備したproduction/test差分はsource locatorを確認して継承し、未Review・未完了の実装候補として評価・必要修正・独立Review・最終Validationする。全履歴への過剰な現契約遡及適用や新管理DBを追加しない。

- 原文の3評価対象と判断基準を現物・既存evidenceから評価し、重複する既存Plan要求の統合案を親へ提示する。他Taskを並行実装しない。
- 重複検証・再走査・再構築・証拠複製・fixtureを削除/統合する案と配置変更を優先比較する。新cache/Stateを前提にしない。
- appはCLI入口と機能接続をownerとし、機能判断・実行・変換とtest/fixtureを適切な既存ownerまたは必要なpackageへ移す。構造の必要性と変更影響も評価する。
- costを実suite/fixture/外部process/lint/Git/archive/直列化に分け、parallelismのメモリ/I/O/競合を含めて評価する。既存計測を優先し、不足部分だけ追加計測する。
- 通常成功と検証FAIL→Fix→再Review→再公開の前後比較を、回数・時間・Codex消費・source binding・保証・測定条件で示す。環境/CI差と実装効果を区別し、単一runだけで高速化を断定しない。
- 重要な責務境界・依存方向・不変条件・残存機構は親が実差分を確認して採否を判断する。通常調査・実装・検証はGLM主体とする。

## Must not

- 必要な独立Review・最終sourceの品質gate・本配置・Publication・Retireを省略しない。古いsnapshotのPASSを転用しない。
- State補正、専用救済操作、timeout延長だけの解決、cache/state増殖、薄いwrapper/過剰interface、app逆依存/循環依存、test移動だけの公開APIを導入しない。
- ファイル分割、timeout分散、package/ファイル/行数削減を高速化成果としない。
- 現在候補の厳格採否と歴史archive保持、接続fixtureと実suite/運用成功を混同しない。
- 調査だけの高コストfull suite反復、大規模一括移動、無関係なNEXT着手を行わない。

## Acceptance criteria

- 外部修正後の起動阻害が恒久解消され、通常経路のsource bindingと旧証拠保全、衝突拒否、次Task開始を確認する。初回復旧にState補正した場合はexact対象と一回限りであることを報告し、通常経路の成功と区別する。

1. 対象検証の目的/owner/回数/時間/実効入力/失効条件を示し、削除・統合・配置変更・証拠利用の採否を判断できる。
2. appと機能ownerの責務・依存・test/fixture配置が整理され、構造改善の理由を示せる。
3. 通常成功とFix/再Reviewの正規経路をState補正なしで確認し、独立故障モード検出と最終source bindingを維持する。
4. 必要な独立Review、検証、最終品質gate、本配置、Publication、Retire、Cleanupを正規経路で成立させる。
5. 実装、時間/Codex効果、構造理由、保証、測定条件、未検証事項を報告し、本Task後は停止する。

## Historical invariants

- 開始時点のsource/CI/運用規則と既存差分を維持し、古い構成へ戻さない。
- 必要な独立故障モードの検出力、現在境界の再検査と最終source品質保証を維持する。

## Dependencies

none
