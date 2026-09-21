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

none

## Resolved references

- `.github/workflows/quality.yml` はrepository全体の通常PR / main CIで、`harnesslint`、`go vet`、`go build`、`go test ./...` を実行する。
- `.github/workflows/web-gpt-validation.yml` は `web-gpt/**` branch pushで起動するWeb GPT専用validationで、Web GPTがローカル実行環境を持たないための代替実行環境である。
- 現状のWeb GPT PRでは、branch pushでWeb GPT Validation、PR eventでRepository Lintが同じheadに対して動き、`harnesslint`、`go vet`、`go build`、`go test ./...` が重複する。
- current `web-gpt-validation.yml` はvalidationを単一job内で直列実行している。
- `web-gpt-validation.yml` はCodex側の開発workflowとして使わない。

## Purpose

Web GPT専用validationとrepository通常CIの責務を整理し、Web GPT開発時に必要な検証coverageを維持しながら、不要な二重実行と直列待ちを減らす。

## Contract

- `web-gpt-validation.yml` はWeb GPTがローカルで実行できないvalidationをGitHub Actionsで実行するためのWeb GPT専用入口として扱う。
- `quality.yml` はrepositoryの通常PR / main CIとして扱う。
- Web GPT PRで両workflowが同じvalidationを無条件に重複実行する必要があるかを整理し、不要な重複は除去する。
- Web GPT専用validationは、独立して実行可能なvalidationをjob分割・並列化することで待ち時間を短縮できるか確認し、適切なら実装する。
- installer smoke / detached runtime identity smokeを含むWeb GPT固有validationの必要coverageは維持する。
- repository通常CIのrequired gateを弱めない。

## Must not

- `web-gpt-validation.yml` をCodex側workflowへ拡張しない。
- Web GPTが必要なremote validation手段を失わせない。
- CI高速化のために必要なvalidation coverageを削除しない。
- unrelated workflow cleanupを本taskへ混ぜない。

## Acceptance criteria

- Web GPT ValidationとRepository Lintの責務境界がworkflow trigger / job構成に反映されている。
- Web GPT PRで不要な同一validation二重実行が残らない、または残す必要がある重複だけが根拠付きで維持される。
- 独立validationを安全に並列化できる場合、Web GPT Validationのjob構成が直列待ちを減らす形になっている。
- Web GPT固有のinstaller smoke / detached runtime identity smokeを含む必要validationが維持される。
- repository通常PR / main CIのgateが維持される。
- Repository Lintと関連workflow validationがPASSする。

## Dependencies

none
