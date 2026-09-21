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

## Resolved references

- `.github/workflows/quality.yml` はrepository全体の通常PR / main CIで、`harnesslint`、`go vet`、`go build`、`go test ./...` を実行する。
- `.github/workflows/web-gpt-validation.yml` は `web-gpt/**` branch pushで起動し、Web GPTがローカル実行環境を持たないためにGitHub Actions上のvalidation実行経路を提供する。
- 現状のWeb GPT PRでは、branch pushでWeb GPT Validation、PR eventでRepository Lintが同じheadに対して動き、`harnesslint`、`go vet`、`go build`、`go test ./...` が重複する。
- current `web-gpt-validation.yml` はvalidationを単一job内で直列実行している。
- installer smoke / detached runtime identity smokeはcurrent `quality.yml` では実行されていない。

## Purpose

Web GPTがローカル実行環境を持たないためのCI validation経路を維持しつつ、必要coverageを漏らさず、同一headでの重複validationを除去し、独立validationを並列化して待ち時間を短縮する。

## Contract

- 必要なvalidation coverageを漏れなく維持する。
- 同じheadに対して同じvalidationを複数workflowで原則重複実行しない。
- `quality.yml` で実行されるvalidationを `web-gpt-validation.yml` で重複実行しない。
- `quality.yml` の通常PR / main gateは弱めない。
- Web GPTがローカル実行環境を持たないために必要なGitHub Actions上のvalidation実行経路を維持する。
- installer smoke / detached runtime identity smokeは、他で実行されておらず必要なcoverageであるため実行を維持する。
- 独立して実行できるvalidationは可能な範囲でjob分割・並列化する。
- 「Web GPT固有validation」と「共通validation」の分類を設計根拠にしない。
- このTaskでは#25を変更せず、#25更新予定をTask本文やPR本文へ追加しない。

## Must not

- 必要なvalidation coverageを削除しない。
- `quality.yml` の通常PR / main gateを弱めない。
- Web GPTが必要なremote validation手段を失わせない。
- validation種別の分類自体を新たな設計・運用contractとして導入しない。
- unrelated workflow cleanupを本taskへ混ぜない。
- #25を変更しない。

## Acceptance criteria

- Web GPT PRの同一headで、`quality.yml` が実行するvalidationを `web-gpt-validation.yml` が重複実行しない。
- 必要なvalidation coverageが維持される。
- installer smoke / detached runtime identity smokeが引き続き実行される。
- 独立validationが安全に並列実行され、不要な直列待ちが減っている。
- `quality.yml` の通常PR / main gateが維持される。
- Web GPTがローカル実行環境を持たない場合のCI validation経路が維持される。
- Repository Lintと関連workflow validationがPASSする。

## Dependencies

none
