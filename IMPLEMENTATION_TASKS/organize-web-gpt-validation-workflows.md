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

## Resolved references

- `.github/workflows/quality.yml` はrepository全体の通常PR / main CIで、`harnesslint`、`go vet`、`go build`、`go test ./...` を実行する。
- `.github/workflows/web-gpt-validation.yml` は `web-gpt/**` branch pushで起動し、Web GPTがローカル実行環境を持たないためにGitHub Actions上のvalidation実行経路を提供する。
- 改修前はWeb GPT PRの同一headで、branch pushのWeb GPT ValidationとPR eventのRepository Lintが `harnesslint`、`go vet`、`go build`、`go test ./...` を重複実行していた。
- installer smoke / detached runtime identity smokeはcurrent `quality.yml` では実行されていない。
- implementation head `a16f5598fdadaa928bd09e7694a56cc2b23e6e16` では、Web GPT Validationをinstaller / detached runtime identityの2 jobへ分離して並列実行できることを実CIで確認済み。
- CI段階化の最終構成は、各validationの実測時間・依存関係・fail-fast可能性を確認して決める。

## Purpose

Web GPTがローカル実行環境を持たないためのCI validation経路と通常PR/main gateを維持しながら、必要coverageを漏らさず、同一headの重複validationを除去し、独立validationを適切に並列化・段階化して、wall-clock待ち時間と失敗headに対する無駄なrunner実行を減らす。

## Contract

- 必要なvalidation coverageを漏れなく維持する。
- 同じheadに対して同じvalidationを複数workflowで原則重複実行しない。
- `quality.yml` で実行されるvalidationを `web-gpt-validation.yml` で無条件に重複実行しない。
- `quality.yml` の通常PR / main gateは弱めない。
- Web GPTがローカル実行環境を持たないために必要なGitHub Actions上のvalidation実行経路を維持する。
- installer smoke / detached runtime identity smokeは、他で実行されておらず必要なcoverageであるため実行を維持する。
- 独立して実行できるvalidationは、依存関係とfail-fast効果を考慮して並列化する。
- `harnesslint` / `go vet` / `go build` / `go test ./...` / installer smoke / detached runtime identity smokeについて実測時間・依存関係・失敗時の短絡可能性を確認する。
- 重いvalidationを常に最初から起動するのではなく、基本gate PASS後へ遅延させる方がrunner wasteを減らす場合はstage依存を導入する。
- validation順序は固定観念で決めず、実測時間と依存関係から選ぶ。
- workflow間依存を実現するために構成変更が必要なら、最小scopeで整理する。
- 「Web GPT固有validation」と「共通validation」の分類を設計根拠にしない。
- このTaskでは#25を変更せず、#25更新予定をTask本文やPR本文へ追加しない。

## Must not

- 必要なvalidation coverageを削除しない。
- `quality.yml` の通常PR / main gateを弱めない。
- Web GPTが必要なremote validation手段を失わせない。
- 実測・依存関係確認なしにvalidation順序を決め打ちしない。
- validation種別の分類自体を新たな設計・運用contractとして導入しない。
- unrelated workflow cleanupを本taskへ混ぜない。
- #25を変更しない。

## Acceptance criteria

- Web GPT PRの同一headで不要なvalidation重複が残らない。
- 必要なvalidation coverageが維持される。
- `harnesslint` / `go vet` / `go build` / `go test ./...` / installer smoke / detached runtime identity smokeの実測時間・依存関係・stage判断が確認されている。
- installer smoke / detached runtime identity smokeが引き続き実行される。
- 独立validationが安全に並列化され、不要な直列待ちが減っている。
- failure-prone / basic gateの失敗headで、後段validationを不要に実行し続けない構成が採用されるか、採用しない合理的根拠が確認されている。
- `quality.yml` の通常PR / main gateが維持される。
- Web GPTがローカル実行環境を持たない場合のCI validation経路が維持される。
- Repository Lintと関連workflow validationがPASSする。

## Dependencies

none
