# Task: post-install probeの実Bundle収録が修正後も失敗する問題を解消する

## Original instruction

2026-11-10 ユーザー指示（原文）:

```text
これらのうちAuditしてないのを順次Auditしてくれ
重複があるかもしれないからそれは省いてくれ
Findingは最後にまとめて俺に報告しろ
Draft PRは立てるな
```

先行するFinding処置指示（原文）:

```text
FindingはIMPLEMENTATION_PLANにタスクを追加するPR作るんだよ
Squash Mergeするんだよ
```

## Amendments

none

## Resolved references

- 前実装Task `post-install-probe-bundle-evidence-fidelity.md` は `21bc1a5bc0db5a163e406f909c2684d8a74f4d2d`で完了・退役し、synthetic positive/negative ZIP testを追加した。
- [Audit #1535](https://github.com/shinderuman/codex-worker-orchestrator/issues/1535)：**修正Task自身**のpost-install run `9386529dab68aa848148885427069ed1` はPASS、source `21bc1a5...`。しかしZIPに `probe.log`がなく、`run.json`に`log_sha256`なし。Go gate用 `gate.log`と`resource-samples.json`が`missing:true`となる。
- [Audit #1539](https://github.com/shinderuman/codex-worker-orchestrator/issues/1539)：**後続Task**のpost-install run `82dd705aa999e35b678492e9d79f3f40`（公開OID `543761685be4c2cb6ccfc06766a4105c3dbfcc21`）でも全く同じ欠測が継続。旧版だけの一回限りの録画ではなく、post-fix organic evidenceでも期待を満たさない。
- [Audit #1542](https://github.com/shinderuman/codex-worker-orchestrator/issues/1542)：さらに後続の要求原文参照Taskで、post-install run `f8aadd725a5840441a699fb6308fe129`（公開OID `9b36afcb5ecda871f65a0c4c2aff7c5c14270fd6`）はPASSだが、`log_sha256`と`probe.log`収録なし、Go-only `gate.log`/resourcesは誤欠測。
- [Audit #1543](https://github.com/shinderuman/codex-worker-orchestrator/issues/1543)：さらに後続のmanaged uninstall Taskでも、run `d98ccb92e69af20e475d3edebc032d0e`（公開OID `aefffcfe56b6e2bfd14fc42fdba8ef69a9c18854`）に同症状。双方の原本ZIP manifest/size/SHA256/CRCは整合し、artifact自体の欠落を否定しない。**4つの異なる修正後source上の実Runでの再発観測**であり、sourceとinstalled binary identityを単に同一と推定しない。
- 修正前の [Audit #1534](https://github.com/shinderuman/codex-worker-orchestrator/issues/1534) は旧コードの既知失敗例であり、新規Findingとして数えない。
- 既存positive fixture `TestPostInstallSmokeProducerToBundleZIP` などがPASSしたことと、installed sourceからの真正RunRecord・collectorによるlive収録を混同しない。原因は旧バイナリ起動/producer/collector/source bindingのいずれか未確定。

## Purpose

post-install smokeが成功した実Taskの証拠を、既存Bundle exporterが`probe.log`と一致するsize/hash/identityで収録し、Go-only artifactの偽欠測を解消する。修正コードが実runtimeに適用されたことを証明する。

## Contract

- `RunRecord`の実生成元（installed `glm-worker`・`glm-parent-action` 等）と実binary hash/source OID、`Log`・`LogSHA256`の書込、selected Task/Attempt/Controller source、Bundle exportのproducer/collectorを原本で追跡し、旧経路のどこが残存したかを再現する。
- 正規`post-install-smoke`は現行`collectGateRun`のform別経路で実`probe.log`を安全に同梱し、存在しないGo gate用`gate.log`/resourcesを「欠測」と数えない。本来のprobe logの欠損・tamper・symbolic link・source/Task/Deployment mismatchではfail-closedで真正欠測/不読を表示する。
- 通常Go test/race・Install Smokeと独立review、既存Bundle manifest/analysis-indexのsource束縛を維持し、data freshnessの欠損を偽PASSにしない。
- 原因がinstall順序/旧プロセスなら本番使用中のbinary更新境界を修正する。過去Bundleの欠測は遡及的に正常化しない。

## Must not

- live効果が未証明なのにsynthetic ZIP testのPASSだけで完了しない。
- バイトのない`probe.log`を空で作る、存在しないresourcesを捏造する、source guardを回避する、旧schema互換専用collectorを増設することをしない。
- 前Task/Issueの実装完了を取り消す、他のtransport/Guardian/Publication修正と混ぜることをしない。

## Acceptance criteria

- installされた現行binaryで**実post-install probe実行→Bundle ZIP export**を一貫して実証し、`run.json`の`log_sha256`と収録`probe.log`のbyte/hash・manifest/analysis-index/source/Task identityが一致する。
- その同じBundleに偽のGo-only missing`gate.log`/resourcesがなく、真正のmissing/unreadable/改ざんは拒否される。通常Go gateとInstall Smokeの証拠は維持。
- #1535/#1539で観測した症状をsource/install/producer/collectorの正しい因果で再現・修正し、関連正負testとrequired Go/race/CI/install smokeを通す。後続の有機的実Taskが未観測ならlive効果の限界を明示する。

## Historical invariants

- `IMPLEMENTATION_RULES.md`とcanonical source-bound gate、現役のみのmachine schema、independent Review、Task1Commit、Go品質保証を維持する。

## Dependencies

none
