# Task: 検索cacheとtracked実contentの一致

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
assume-unchanged等のindex flagがあっても、repo-searchのcache-hitを古い内容の正当化に使わない。検索corpusとfingerprintの意味を一致させる。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

### 2026-11-03 Codexの先行採否

採否原文: https://github.com/shinderuman/codex-worker-orchestrator/pull/1484#issuecomment-6588681688

````text
Codexの先行設計判断です。確認対象はhead `d3ae9d58f8ce5bf06ac740669287289ace36c2fc`、基点は `e3dcec1525adbee32e33078928510c94dcdbb404` です。

**unsupported index flagsの明示拒否方式を採用します。検索corpus全量のraw contentを毎回読み直す方式は、本Taskでは採用しません。** 以下の範囲で本PRを完成させてください。

### 1. 拒否条件とcorpus

対象は既存index構築corpusの候補となる、index mode `100644` / `100755` のtracked entriesです。既存のdefault/明示ExcludeDirsに属するpathは除外し、symlink（120000）・gitlink/submodule（160000）は既存の非検索対象のままにします。flagを理由にこれらの対象を新たに検索へ含めません。

この候補集合にassume-unchangedまたはskip-worktreeがあれば、`ErrUnsupportedIndexFlags` による明示拒否を採用します。`-v -s -z --cached` の通常H以外を成功entryとして扱わない現方針でよく、不正な出力や未対応状態も空集合へ縮退させません。[Git公式仕様](https://git-scm.com/docs/git-ls-files)でも、Hは非unmerged・非skip-worktreeのtracked entry、Sはskip-worktree、-vの小文字はassume-unchangedです。ただしHを「fsmonitor等も含めたあらゆるindex flagがない」証明とは説明しないでください。本件の確認対象を超えた保証はしません。

`PathPrefixes` / `Symbols` は既存実装ではindex構築後の結果絞込みなので、それだけを理由にflag検査対象を狭めません。queryにhitしないfileもindex候補なら拒否対象です。後段のbinary/size等のskip判定のためだけに、先に全fileを読んでflag検査を避ける必要もありません。

### 2. sparse checkoutの採否

**検索corpus内にskip-worktreeがあるsparse checkoutは非対応として拒否**します。sparse checkout設定が存在するだけでrepository全体を一律拒否する必要はありません。skip-worktreeが既存の除外directory/非検索対象entryにしかない場合は、残る正規corpusの検索を許可してよいです。

sparse-indexも同じ境界で確認してください。Gitは通常、ls-filesでsparse directoryを配下fileへ展開し、`--sparse` を指定するとdirectory自体を表示します（[公式仕様](https://git-scm.com/docs/git-ls-files)）。sparse-directory entryを非regularとして捨てた結果、配下の非対応状態を見逃す実装にはしません。既存のsparse fixtureを使い、通常index/sparse-indexと除外corpusの境界を必要な範囲で確認してください。

### 3. cache・同時変更・errorの意味

enumerationとfingerprintが同じ検査ownerを使う変更を採用します。cache-hitの前、rebuild、検索後のfreshness検査、cached/uncached/exhaustiveで同じ契約を維持してください。

途中でflagが現れた場合も、古いresultやexhaustive成功を返さずerrorにします。flag解除後は通常のfreshness判定でhit/rebuildへ復帰させ、古いcacheの推定promotion、旧形式migration、modelによる再検索fallbackは追加しません。利用側が自動でindex flagを解除したりsparse checkoutを変更して通す処置も不要です。

errorをnormal empty resultへ変換しないことを、standalone/parent向け入口でも維持してください。unsupported時にdelivery済み・exhaustive完了の証拠を残してはいけません。

### 4. CI・費用測定・仕上げ

[最終headのpush CI #41524599428](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41524599428)は、確認時点で基本5系統、集約lint、install-smoke 4系統の**計10ジョブsuccess**でした。以前のcommitからの推定ではなく、このrunの結果を採用します。

[測定run #41524278995](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41524278995)のsuccessと、128 files・8反復のログも確認しました。既存ls-files呼出しへのflag追加で、通常経路のGit subprocess回数を増やさない点はコードから確認できます。一方、少数試行のsub-ms差から性能改善/劣化やCodex削減率は判断しません。cached Searchの絶対時間を旧実装との比較値にはしません。

`TestRepoSearchIndexFlagsCostRepresentativeCorpus` は時間を記録するために通常suiteで列挙比較とcache-hitを反復しており、時間に合否基準がありません。測定結果は今回の証拠として利用し、継続計測を残すならBenchmarkへ分離してください。通常のcorrectness回帰は既存のflag拒否・解除後のhit/rebuild・検索中変更・exhaustiveのassertを残し、同じcache-hitを時間Logのためだけに繰り返す通常Testは整理します。

以上により設計判断待ちは解除します。scope境界の確認と計測Test整理で変更した場合は、その最終SHAで必要な関連test/lintを確認してください。今回はMerge承認や、実ユーザー環境への配置済み判定ではありません。統合時の必要な本配置・installed/source一致・smokeと最終採否はCodexが担当します。

Task/Planは親Codex専有なのでWeb GPT側で先行削除せず、必要な更新候補と根拠を返してください。checkpoint先頭の順序も維持します。
````

## Resolved references

- `Review.md` の「2026-10 再監査」の A7。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

assume-unchanged等のindex flagがあっても、repo-searchのcache-hitを古い内容の正当化に使わない。検索corpusとfingerprintの意味を一致させる。

## External feasibility

status: not-applicable

## Contract

- 一時repoでassume-unchangedを付けたtracked fileをcache構築後に変更すると、cached検索だけ0件になる不具合を修正する。
- tracked raw contentを反映する方式か、対応できないflagを入口で明示拒否する方式かを親が確定する。skip-worktree/sparse checkoutの対応範囲も明示する。
- cache keyだけでなく、検索前後の変更検査とexhaustive表示も同じcorpus契約へ合わせる。controller側の別guardをstandalone検索入口の代替にしない。

## Must not

- stale cacheから0件を成功として返さない。unsupported状態を通常empty resultへ縮退しない。
- 旧cacheのmigration、推定promotion、追加model callによる再検索fallbackを追加しない。
- 毎回のrepository全走査を費用確認なしで採用しない。

## Acceptance criteria

- assume-unchanged付きfileのoldneedle→newneedle変更で、cached/uncached検索が同じ正規結果になるか、unsupportedとして明示拒否される。
- skip-worktree/sparseの有無、flag切替、検索中変更、通常cache-hitを検証する。
- correctnessとexhaustive表示を保ち、代表的corpusでcache費用への影響を測定し、related testsとquality gateを通す。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
