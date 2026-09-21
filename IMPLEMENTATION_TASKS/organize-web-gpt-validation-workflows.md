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
整理の軸は「Web GPT固有validation」と「共通validation」の分類ではなく、必要coverageを漏れなく維持し、同じheadで同じvalidationを原則1回だけ実行し、独立validationを可能な範囲で並列化すること。
`quality.yml` の通常PR/main gateは弱めず、Web GPTがローカル実行環境を持たないためのCI実行経路を維持する。
installer smoke / detached runtime identity smokeは「Web GPT固有だから」ではなく、他で実行されていない必要coverageだから残す。
#25は変更せず、Task本文やPR本文へ#25更新予定も追加しない。
```

```text
CIを段階化し、harnesslint / go vet / go build / go test ./... / install_smoke / install_detached_runtime_identity_smokeについて、実行時間、失敗時の短絡、依存関係、stage順序を実測に基づいて整理する。
基本gateが失敗するheadで重いsmokeを最初から実行する必要があるか検討する。
workflow構成変更が必要なら最小scopeで行う。
必要coverageは削らず、quality.ymlの通常PR/main gateは弱めず、#25は変更しない。
```

```text
CI全体の実行コストを棚卸しする。
Repository Lint内部のquality/lint系とfull Go testの分離、quality tools install重複、setup-go / Go cache、validation間の実質的重複、matrix fail-fast、concurrency.cancel-in-progressを実測・依存関係・runner waste・job setup overheadで比較する。
効果がない、または複雑化の方が大きい項目は変更しない。
```

```text
追加で、Workflowのファイル名と起動タイミングも整理すること。

1. Workflowファイル名を現在の責務に合わせる。
現状の `.github/workflows/quality.yml` / `.github/workflows/web-gpt-validation.yml` について、少なくとも `.github/workflows/ci.yml` / `.github/workflows/install-smoke.yml` の方向で検討する。
`ci.yml` はPR/main CI全体の入口・オーケストレーター、basic quality、full Go test、required gate aggregator、条件を満たした場合の後段smoke呼び出しを担当する。
`install-smoke.yml` はinstaller smokeとdetached runtime identity smokeをreusable workflowとして担当する。
ファイル名はWeb GPTという分類ではなく実際の責務を表す。
Workflow表示名やjob/check名まで機械的に変更しない。特に `Repository Lint / lint` のrequired check影響を確認し、壊す可能性があるなら表示名/check名は維持する。

2. Web GPT branchの起動タイミングを再確認する。
現在案で `web-gpt/**` push triggerを削除すると、Draft PR作成前のbranch pushでは自動validationが走らなくなるため、そのまま確定しない。
branch作成後Draft PR前にpushするケース、その期間のremote validation要否、早期Draft PRを恒久前提にしてよいか、PR CIとbranch push CIを両方有効にした場合の同一head二重実行を確認する。
Web GPT branch push / PR open・synchronize / main push / manual dispatchの各eventで何を走らせるかを明示して決める。
目標は、PR作成前のremote validation手段を失わず、PR作成後は同一headで同じvalidationを二重実行せず、main通常CIを維持し、manual dispatchを必要なら残すこと。
単純に `web-gpt/** push` triggerを戻してPR CIと二重実行させない。
必要なら branch pushでは軽い/basic validationのみ、PRではrequired CI+後段smoke、または早期Draft PRを恒久前提としてbranch push自動起動を不要とする案を比較する。

3. 最終確認。
Workflowファイル名と責務が一致すること、required check契約を壊さないこと、PR作成前validation不能の穴がないか意図的廃止の根拠があること、PR作成後の同一head二重実行がないこと、main CIが成立すること、manual dispatchが必要なら使えること、実CIでtriggerとstage順序を確認すること。
#25は変更しない。
```

## Resolved references

- 改修前はWeb GPT PRの同一headでRepository LintとWeb GPT Validationが `harnesslint` / `go vet` / `go build` / `go test ./...` を重複実行していた。
- installer smoke / detached runtime identity smokeは通常CIの4 gateとは別のcoverageである。
- 実測では旧Repository Lint代表run約163秒、旧Web GPT Validation単一job約262秒。
- quality/lint系とfull Go testを並列化し、basic PASS後だけsmokeを起動する構成は実CIで成立した。
- quality-tools cacheは実測効果があり、Go cacheは安全側比較で有意差がなかったため不採用とした。
- current PRでは既存 `Repository Lint / lint` check名をaggregatorで維持している。
- current PRでは `web-gpt-validation.yml` のbranch push triggerを外し、PR event由来の `quality.yml` からreusable workflowとして後段smokeを呼ぶ構成になっているため、PR前remote validationとtrigger重複を再評価する必要がある。

## Purpose

Web GPTがローカル実行環境を持たないためのremote validation経路と通常PR/main gateを維持しながら、必要coverageを漏らさず、重複validation、不要な直列待ち、重複setup/installを削減する。さらにWorkflow名とtriggerを実際の責務・運用に一致させ、PR前・PR後・main・manualの各経路でvalidationの穴と二重実行をなくす。

## Contract

- 必要なvalidation coverageを漏れなく維持する。
- 同じheadに対して同じvalidationを複数workflowで原則重複実行しない。
- 通常PR/main gateを弱めず、既存required check契約を壊さない。
- Web GPTがローカル実行環境を持たないためのremote validation経路を維持する。
- `concurrency.cancel-in-progress: true` を維持する。
- basic qualityとfull Go testの並列化、後段smoke stage、quality-tools cacheは実測根拠を維持する。
- Workflowファイル名を責務に合わせて整理し、少なくとも `ci.yml` / `install-smoke.yml` 案を評価する。
- Workflow表示名・job/check名はrequired check影響を確認した上で必要最小限のみ変更する。
- Web GPT branch push / PR open・synchronize / main push / manual dispatchそれぞれの実行内容を明示する。
- PR前remote validationの穴を作らず、PR後に同一headの同一validationを重複実行しない。
- branch pushとPR triggerを併存させる場合、event条件で二重実行を避ける。
- manual dispatchは用途を確認し、必要なら維持する。
- #25を変更しない。

## Must not

- 必要なvalidation coverageを削除しない。
- 通常PR/main gateを弱めない。
- required check契約を不用意に変更しない。
- Web GPTが必要なremote validation手段を失わせない。
- 単純に `web-gpt/** push` を戻してPR CIとの二重実行を復活させない。
- 実測・依存関係確認なしにtrigger、validation順序、cache、artifact共有を決め打ちしない。
- runner消費を無視してwall-clockだけを最適化しない。
- validation種別の分類自体を設計根拠にしない。
- unrelated workflow cleanupを混ぜない。
- #25を変更しない。

## Acceptance criteria

- Workflowファイル名と実責務が一致している。
- `Repository Lint / lint` を含むrequired check契約が維持されるか、変更が安全と確認されている。
- Web GPT branch push / PR open・synchronize / main push / manual dispatchの実行内容が明示されている。
- PR作成前のremote validationに穴がない、または早期Draft PR恒久前提への変更根拠が明示されている。
- PR作成後に同一headの同一validation二重実行がない。
- main CIが成立する。
- manual dispatchを残す場合は使用可能である。
- basic quality / full Go test / aggregator / installer smoke / detached runtime identity smokeのcoverageが維持される。
- 実CIでtriggerとstage順序を確認する。
- whole-diff self-reviewとRepository Lint関連validationがPASSする。

## Dependencies

none
