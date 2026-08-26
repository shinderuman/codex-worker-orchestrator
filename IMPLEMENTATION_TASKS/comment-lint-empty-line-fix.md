# Task: comment lint fixの不要空行生成を止める

## Original instruction

```text
あとinstall_smoke.sh とかを見ると無駄な改行がある

LinterのFixがバグっているようでシェルスクリプトに無駄な空行がある
空行がある事自体はそんなに気にしていない
元のクソコメントよりは無駄なバイトを消費しないだろうから

Linter自体の修正はやってほしい
今後についてはわざわざフォーマッタを用意したりはしたくない
そっちのコストのほうが無駄だと思うから

既存空行についても多分スモークテスト肥大化対応とかの作業で消えると思っている
その時にでもついでに消えてくれればいい
```

## Amendments

### Amendment 1

```text
GLMのPushは作業は禁止のままだが今回のGreptile導入に伴いCodexへのPushを許可する
```

## Resolved references

- 「Linter」はsource comment全面禁止taskで導入・使用したcomment lint/fix経路を指す。
- 「スモークテスト肥大化対応」は`IMPLEMENTATION_TASKS/install-smoke-loop-cost-reduction.md`を指す。

## External feasibility

status: not-applicable

## Purpose

comment lintのfixがcomment削除後へ不要な空行を残す原因を修正し、今後の自動fixで同じ空行を生成しないようにする。

## Contract

- comment lint/fixのproducerを一次証拠から特定し、comment削除時の行境界処理だけを最小修正する。
- shell scriptを含む対象言語で、comment削除が不要な連続空行を新規生成しないことをtestで固定する。
- lint detectionとcomment禁止contractは維持する。

## Must not

- 汎用formatter、全file整形、空行禁止ruleを新設しない。
- 既存空行の一括cleanupを本taskの主成果にしない。
- 空行削減を理由にscriptの意味や既存quality gateを変えない。

## Acceptance criteria

- comment lint fixの不要空行生成原因が修正され、再現testが通る。
- lint/check/fixの既存contractと関連testが通る。
- 既存空行は関連fileを別taskで編集する際の自然なcleanupに留め、一括formatterを追加しない。
- GLMはcommit/pushせず、完了gate後に親Codexがcommitし、repository恒久許可どおりremote mainへ通常fast-forwardする。

## Historical invariants

- source comment禁止contract自体は維持する。

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。5h wake再予約lifecycle修復の完了後に昇格し、comment lint fixの不要空行生成だけを修正する。
