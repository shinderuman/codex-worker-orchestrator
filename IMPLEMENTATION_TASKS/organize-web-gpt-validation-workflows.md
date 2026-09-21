# Task: organize Web GPT validation workflows

## Original instruction

```text
常に2個動く必要あるのか
分割したらCIが早くなったりしないのか

このWorkflowはCodex側では使ってないからな
このWorkflowはWeb GPTで開発する際ローカルとかがないからCIでやらざるを寝合いだけなんだから

じゃあ整理するタスクも作ってくれ
さっきのやつの前でいいと思う
```

## Amendments

```text
整理の軸を修正する。

今回やることは「Web GPT固有validation」と「共通validation」を分類することではない。
その分類は不要。

目的は次の2点。

1. 無駄をなくす
   - 同じheadに対して同じvalidationを複数Workflowで重複実行しない。
   - `quality.yml` で実行されるvalidationを `web-gpt-validation.yml` でも重複実行する必要はない。
   - 必要なvalidation coverage自体は削らない。
2. validationを効率化する
   - 独立して実行できるvalidationは可能な範囲で並列化する。
   - installer smoke / detached runtime identity smokeについても、「Web GPT固有だから残す」のではなく、他で実行されておらず必要なvalidationだから実行する。
   - それらが独立しているなら並列実行する。

要するに、

- 必要なvalidationを漏れなく実行する
- 同じvalidationは原則1回だけ実行する
- 独立したvalidationは並列化する
- `quality.yml` の通常PR/main gateは弱めない
- Web GPTがローカル実行環境を持たないために必要なCI実行経路は維持する

という形に整理する。

「Web GPT固有validation」という概念を設計根拠にしないこと。

また、このTaskでは#25を変更しない。
Task本文やPR本文へ#25更新予定などの記載も追加しない。
```

```text
追加でCIの段階化も検討すること。

今回の目的は、単に重複validationを消して並列化することだけではない。
CI全体として、不要な重いvalidationを早い段階から毎回同時実行しない構成に整理する。

確認すること:

- `harnesslint`
- `go vet`
- `go build`
- `go test ./...`
- `install_smoke`
- `install_detached_runtime_identity_smoke`

これらについて、

1. 実行時間
2. 失敗時に後続を止められるか
3. 他validationへの依存関係
4. どの順序・stageにすると無駄な実行時間を減らせるか

を確認する。

特に、基本的なlint / build / testが失敗するheadに対して、重いsmokeまで最初から並列実行する必要があるのかを検討すること。

必要なら、

- 早く失敗を検出する基本gate
- 基本gate PASS後に走らせる重い/統合的validation

のようにstageを分ける。

ただし「Go testを必ず最初にする」など順序を固定で決め打ちしないこと。
実測時間と依存関係に基づいて、全体の待ち時間と無駄なrunner実行を減らす構成を選ぶ。

現在 `quality.yml` と `web-gpt-validation.yml` が別Workflowであるため、stage依存を実現するのにWorkflow構成変更が必要なら、その点も含めて最小scopeで整理すること。

必要validation coverageは削らない。
`quality.yml` の通常PR/main gateは弱めない。
#25は変更しない。
```

```text
今回のTaskについて、stage化だけで終わらず、CI全体の実行コストをもう一度棚卸しすること。

現時点の実測では以下が見えている。

- Repository Lint
  - quality tools setup/install: 約30秒
  - harnesslint: 約45秒
  - go vet: 約7秒
  - go build: 約1秒
  - go test ./...: 約109秒
- Web GPT Validation
  - installer smoke jobでもquality toolsをsetup/install
  - detached-runtime-identity smoke jobでも同じquality toolsをsetup/install
  - このsetup/installが各jobで約22〜24秒重複している

したがって、以下も今回の整理対象として確認する。

1. Repository Lint内部の直列実行
   - `go test ./...` はquality toolsのinstallを必要としないはずなので、harnesslint等と同じjobで直列に待つ必要があるか確認する。
   - 例えば、
     - quality/lint系job
     - Go test job
       を並列化し、その両方のPASSを後段stageの前提にする方がwall-clockとfail-fastの両面で有利か検証する。
   - `go build`は約1秒、`go vet`も約7秒なので、job分割によるrunner/setup overheadの方が大きいなら無理に分割しない。
2. quality tools installの重複
   - installer smokeとdetached-runtime-identity smokeが別jobになることで、同じquality toolsを2回ゼロからinstallしている。
   - 並列化でwall-clock上隠れていてもrunner消費としては重複なので、これを放置する必要があるか確認する。
   - cache、artifact共有、job構成変更などを比較し、複雑化に見合う実測改善がある方法だけ採用する。
   - 「cacheを入れれば速いはず」のような推測だけで導入しない。
3. setup-go / Go cache
   - 現在 `cache: false` になっている理由と影響を確認する。
   - `go test ./...` の約109秒を安全に短縮できるcache構成があるか実測する。
   - 再現性やvalidationの信頼性を落とすなら採用しない。
4. validation間の実質的な重複
   - `go build ./...`、`go test ./...`、その他gateがそれぞれ何を追加で保証しているか確認する。
   - 実質的に同じ証拠しか出していないものがあるか確認する。
   - ただし、等価性を確認できないgateを「たぶん重複」として削除しない。
   - `quality.yml` の通常PR/main gateを弱めないという既存条件を守る。
5. matrixのfail-fast設定
   - 現在smoke matrixは `fail-fast: false`。
   - 一方が失敗した時にも他方を最後まで走らせる価値とrunner wasteを比較する。
   - 診断情報を得る価値が高ければfalseのままでよい。単なる無駄なら変更を検討する。
   - ここも推測ではなく実際の用途で判断する。
6. 既に有効な仕組みは壊さない
   - `concurrency.cancel-in-progress: true` は新しいpushで古いrunを打ち切るため有効なので維持する。
   - 必要coverageを削らない。
   - Web GPTがローカル実行環境を持たないためのCI validation経路を維持する。
   - #25は変更しない。

最終的には単に「jobを増やして並列化した」という状態ではなく、

- 成功時のwall-clock
- 失敗headで無駄に消費するrunner時間
- 重複setup/install
- job分割そのものによるsetup overhead

を合わせて見て、現在より実際に効率が上がる構成へ整理すること。

効果がない、または複雑化の方が大きい項目は「変更しない」という結論でよい。
```

## Resolved references

- `.github/workflows/quality.yml` はrepository全体の通常PR / main CIで、quality tool setup/installの後にownership smoke、`harnesslint`、`go vet`、`go build`、`go test ./...` を単一jobで直列実行する。
- `.github/workflows/web-gpt-validation.yml` は `web-gpt/**` branch pushで起動し、Web GPTがローカル実行環境を持たないためにGitHub Actions上のvalidation実行経路を提供する。
- 改修前はWeb GPT PRの同一headで、branch pushのWeb GPT ValidationとPR eventのRepository Lintが `harnesslint`、`go vet`、`go build`、`go test ./...` を重複実行していた。
- installer smoke / detached runtime identity smokeはcurrent `quality.yml` では実行されていない。
- implementation head `a16f5598fdadaa928bd09e7694a56cc2b23e6e16` では、Web GPT Validationをinstaller / detached runtime identityの2 jobへ分離して並列実行できることを実CIで確認済み。
- 同headの実CIではinstaller validation本体約36秒、detached runtime identity validation本体約17秒で、各jobにquality tools setup/install約22〜25秒が重複する。
- Repository Lint実CIではquality tools setup/install約30秒、harnesslint約45秒、go vet約7秒、go build約1秒、full Go test約109秒が支配的である。
- `go test ./...` 自体はrepositoryのquality tool binariesを呼ぶworkflow stepではなく、quality tool installから独立してjob分離可能かを確認対象とする。
- CI段階化・cache・artifact共有・job統合/分割の最終構成は、実測時間・依存関係・fail-fast効果・setup overheadを比較して決める。

## Purpose

Web GPTがローカル実行環境を持たないためのCI validation経路と通常PR/main gateを維持しながら、必要coverageを漏らさず、同一headの重複validation、不要な直列待ち、重複setup/installを減らし、成功時wall-clockと失敗headのrunner wasteの両方を実測ベースで改善する。

## Contract

- 必要なvalidation coverageを漏れなく維持する。
- 同じheadに対して同じvalidationを複数workflowで原則重複実行しない。
- `quality.yml` で実行されるvalidationを `web-gpt-validation.yml` で無条件に重複実行しない。
- `quality.yml` の通常PR / main gateは弱めない。
- Web GPTがローカル実行環境を持たないために必要なGitHub Actions上のvalidation実行経路を維持する。
- `concurrency.cancel-in-progress: true` を維持する。
- `harnesslint` / `go vet` / `go build` / `go test ./...` / installer smoke / detached runtime identity smokeについて、実測時間、依存関係、追加で保証するevidence、失敗時の短絡可能性を確認する。
- `go test ./...` とquality/lint系のjob分離はwall-clock短縮と追加setup overheadを比較して決める。短い`go vet` / `go build`を単独jobへ過分割しない。
- installer smoke / detached runtime identity smokeのquality tools install重複について、job統合、artifact/cache共有等を比較し、複雑化に見合う実測改善がある方法だけ採用する。
- setup-goの`cache: false`の由来・意図を確認し、Go module/build/test cacheの安全性と実測効果を確認してから変更可否を決める。
- `go build` / `go test` / その他gateのevidenceが等価と確認できない限りcoverageを削除しない。
- smoke matrixの`fail-fast`は診断価値とrunner wasteを実用途で比較して決める。
- 重いvalidationを常に最初から起動するのではなく、basic gate PASS後へ遅延させる方がrunner wasteを減らす場合はstage依存を導入する。
- workflow間依存を実現するために構成変更が必要なら、最小scopeで整理する。
- 「Web GPT固有validation」と「共通validation」の分類を設計根拠にしない。
- このTaskでは#25を変更せず、#25更新予定をTask本文やPR本文へ追加しない。

## Must not

- 必要なvalidation coverageを削除しない。
- `quality.yml` の通常PR / main gateを弱めない。
- Web GPTが必要なremote validation手段を失わせない。
- 実測・依存関係確認なしにvalidation順序、cache、artifact共有を決め打ちしない。
- runner消費を無視してwall-clockだけを最適化しない。
- validation種別の分類自体を新たな設計・運用contractとして導入しない。
- unrelated workflow cleanupを本taskへ混ぜない。
- #25を変更しない。

## Acceptance criteria

- Web GPT PRの同一headで不要なvalidation重複が残らない。
- 必要なvalidation coverageが維持される。
- 6 validationの実測時間・依存関係・evidence差・stage判断が確認されている。
- Repository Lint内部のjob分割について、成功時wall-clockと追加setup overheadの比較に基づく結論がある。
- smoke間のquality tools install重複について、job統合 / cache / artifact共有の比較に基づく結論がある。
- setup-go / Go cacheについて、現行`cache: false`の意図と実測効果・信頼性に基づく結論がある。
- matrix `fail-fast`について診断価値とrunner wasteに基づく結論がある。
- failure-prone/basic gateの失敗headで、後段の重いvalidationを不要に実行し続けない構成が採用されるか、採用しない合理的根拠が確認されている。
- `quality.yml` の通常PR / main gateが維持される。
- Web GPTがローカル実行環境を持たない場合のCI validation経路と`concurrency.cancel-in-progress`が維持される。
- Repository Lintと関連workflow validationがPASSする。

## Dependencies

none
