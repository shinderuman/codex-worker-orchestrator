# Task: User-level installation scope redesign

## Original instruction

```text
さっきのタスクとは別にcodex/AGENTS.md を見直すというタスクを作っておいてくれ
優先度は高くない
そもそもこのリポジトリの内容でユーザーレベルのAGENT.mdを書き換えてるのは責務としておかしい
ツールを使う場合に読み込まれるAGENT.mdとかそういう再設計が必要だと思う
```

## Amendments

### 2026-09-04

```text
AGENT.mdに限らずユーザーレベルを強制しているものがあればそれも含めろ
```

```text
検証するタスクでいいがNo-Goの判断はなしだ
```

### 2026-09-04 priority correction

```text
その位置だと105の後の再評価、のタスクでタスクが積まれた場合に作業が永久にされない可能性があるだろ
```

## Resolved references

- 現行`./install.sh`はrepositoryの`codex/AGENTS.md`をユーザーレベルの`~/.codex/AGENTS.md`へ全面配置する。
- ユーザー記載の`AGENT.md`は、現行file名である`AGENTS.md`を指すものとして扱う。
- `auto-resume-heartbeat-transaction.md`の恒久許可実効性とは別責務であり、本taskはCodex instruction全体のownershipと読み込みscopeを扱う。
- 対象は`AGENTS.md`だけでなく、このrepositoryのinstaller、setup、commandがuser homeまたはuser-wide設定へ書き込み・上書き・merge・削除する全surfaceとする。
- `post-105-codex-efficiency-reevaluation.md`は新規findingを022前へ追加して作業サイクルを継続し得るため、その後ろでは本taskが継続的に後回しになる。105直後の再評価を維持するため、本taskは`105-session-rotation.md`の直前に置く。

## Purpose

repository固有・tool利用時固有・ユーザー全体の設定を適切なscopeへ分離し、このrepositoryがユーザー所有のグローバル設定を不必要に強制・上書きする責務不整合を解消する。

## External feasibility

status: observation

Codexがtool利用時またはproject利用時だけinstructionを読み込むsupported mechanismはrepository外の製品境界を含む。local runtime・設定・既存実装を一次証拠として確認し、不足時だけ公式資料で成立性を検証する。特定候補が不成立でも本task自体をNo-Go終了せず、成立する別方式を選んで責務分離を実装する。

## Contract

- repository内のinstaller、setup、uninstall、commandからuser-level pathまたはuser-wide設定への全mutationを棚卸しし、対象、操作種別、owner、consumer、必要scope、rollbackを記録する
- `codex/AGENTS.md`の各規則と`codex/instructions/`、repository root `AGENTS.md`、GLM prompt / command contextのconsumerを棚卸しし、user-global、repository-specific、tool-specific、on-demand instructionへ分類する
- `~/.codex/AGENTS.md`をrepositoryが全面所有・上書きする現行責務を見直し、ユーザー固有指示とrepository配布物のownershipを分離する
- `~/.codex`以外も含め、既存ユーザー設定をrepository都合で強制するsurfaceと、namespacedなtool-owned binary/state/configを区別し、不適切なglobal mutationを分離または廃止する
- glm-workerまたは関連toolを使う場合だけ必要な指示を確実に読み込ませるsupported mechanismを調査し、暗黙の会話memoryや常時global注入に依存しない構成を比較する
- instruction precedence、project外taskへの漏出、tool未使用taskのtoken負荷、install / upgrade / uninstall、既存ユーザー設定のmigrationとrollbackを設計対象に含める
- architecture、公開される設定責務、互換性、migration方針は実装前にSol Highが採用案を決定する
- tool-scoped loading候補が外部製品境界で成立しない場合は、その案だけを除外し、project scope、明示include、namespaced config等のsupported alternativeから責務分離を実装する

## Must not

- `~/.codex/AGENTS.md`の既存ユーザー内容を無断で削除・置換・repository規則だけへ縮退しない
- `AGENTS.md`だけを直して他のuser-level強制surfaceを未調査のまま残さない
- repository固有規則を別のglobal fileへ移すだけで責務分離済みと扱わない
- 同じ規則をglobal、repository、tool promptへ重複配置して複数正本を作らない
- supported loading behaviorを未検証の推測で設計前提にしない
- 個別候補の不成立を理由に本task全体をNo-Goまたは現状維持で終了しない
- GLM worker/reviewerのGit remote write禁止、parent/worker responsibility、instruction self-protectionを意図せず弱めない

## Acceptance criteria

- user-levelへmutationする全surfaceについてowner、必要consumer、必要scope、操作種別、移行先または残置理由が追跡可能なbounded artifactとして示される
- 現行`codex/AGENTS.md`の規則ごとにowner、必要consumer、必要scope、移行先または残置理由が追跡可能である
- tool / project scoped instruction loading候補の成立性がlocal evidenceと必要時の公式資料で検証され、Sol Highが実装可能な採用案を確定する
- 採用案では、既存のユーザー固有`~/.codex/AGENTS.md`内容を保持したままinstall / upgrade / uninstallできる
- `AGENTS.md`以外の既存ユーザー設定も、明示的にtool-ownedと確認された範囲を除き、install / upgrade / uninstallで無断強制・破壊されない
- repository外かつglm-worker未使用のCodex taskへrepository固有規則が不要に注入されない
- glm-worker利用時と本repository作業時には必要な規則が欠落せず読み込まれることをintegration testまたは実機scenarioで確認する
- instruction precedence、migration、rollback、旧managed manifestの扱いがtestされ、二重正本やsilent overwriteが残らない
- 検証結果に基づく責務分離のproduction変更とmigrationを実装し、現状維持または調査報告だけで完了しない
- harnesslint、関連test、独立review、必要なcurrent snapshot validationを完了する

## Historical invariants

- ユーザーレベルの設定はユーザー所有であり、repositoryの都合だけで全面上書きしない。
- 常時読み込むinstructionは全taskに必要な最小集合とし、tool固有詳細は必要時だけ読む。
- Sol High品質を維持しつつ、無関係なglobal instruction注入によるCodex token消費を増やさない。

## Dependencies

none

## Review findings

none

## Current boundary

低優先度。既存のpre-105 task群の後、`105-session-rotation.md`の直前に検証・設計・実装する。No-Go完了は許可しない。
