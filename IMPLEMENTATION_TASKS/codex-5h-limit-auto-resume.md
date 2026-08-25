# Task: 親Codex 5h Limitを自動再開する

## Original instruction

`````text
# Codexへの指示：親Codex 5h Limitの自動再開をGLM-Worker機能として追加する

GLM-Worker利用中に**親Codexが5h Limitへ到達した場合、reset後にユーザー操作なしで親実装セッションを再開する機能**を追加する。

これはこのリポジトリ自身の開発専用機能ではなく、**GLM-Workerを利用する任意の開発で使えるGLM-Worker本体の機能**として設計すること。

## 前提

既存構成は以下。
```text
親実装Codexセッション
└─ GLM Resume scheduler（既存・同じ実装セッション）

Greptile専用Codexセッション
└─ Greptile scheduler（既存）

Codex 5h wake専用Codexセッション
└─ 今回追加するscheduler
```

- 既存GLM Resumeは変更しない。
- Greptileの既存構成も変更しない。
- 同一Codexセッションへ複数schedulerを持たせず、Codex 5h wake専用の別セッションを使う。
- Weekly Limitからの自動復旧は今回の対象外。
- 追加クレジット利用は前提にしない。
- 親Codexが利用不能な間にGLMだけで開発を継続しない。

## 基本設計

**Codexの行動規則はMarkdownを主とする。**

Goへ実装するのは、Codex自身へ文章で解析させるより機械処理した方が確実な部分だけに限定する。

責務は概ね以下。
```text
Markdown / Codex
├─ 5h Limit時の停止規則
├─ wake専用セッションの運用
├─ scheduler削除・作成
├─ 親実装セッションへの再開指示
└─ 次回wake設定

Go
└─ 必要な場合のみ
   └─ Codex rate-limit情報を機械取得してJSONで返す
```

scheduler操作そのものをGoへ実装しない。

人間向けのscheduler作成・削除CLIも作らない。手動介入時のscheduler操作はCodex Desktopの既存UIを使用する。

## 5h Limit情報取得

手作業PoCですでに現在のCodex CLIで以下が成立することを確認済み。
```text
codex app-server
→ initialize
→ initialized
→ account/rateLimits/read
```

実レスポンスでは `limitId == "codex"` に、
```json
{
  "primary": {
    "usedPercent": 100,
    "windowDurationMins": 300,
    "resetsAt": 1787685137
  },
  "secondary": {
    "usedPercent": 16,
    "windowDurationMins": 10080,
    "resetsAt": 1788271937
  },
  "planType": "plus",
  "rateLimitReachedType": "rate_limit_reached"
}
```

が返ることを確認している。

したがって、5h windowは `windowDurationMins == 300` を使って識別できる。

ただし実装では `primary == 5h` を前提にせず、`primary` / `secondary` の内容から `windowDurationMins == 300` のwindowを選ぶこと。

必要ならGLM-Worker側に、たとえば以下のような**読み取り専用・machine JSON出力の薄い機能**を追加してよい。
```text
glm-worker codex-limit
```

目的は現在の5h / Weekly状態と `resetsAt` を機械的に取得することだけ。

schedulerの作成・削除、親セッションへの送信までこのコマンドへ詰め込まない。

## 5h Limit到達時

親実装Codexが5h Limitへ到達したら、GLMを含め開発全体を停止する。
```text
通常開発
↓
親Codex 5h Limit
↓
全体停止
↓
5h reset
↓
wake専用Codexセッション発火
↓
親実装セッションへ「作業を続けろ」
↓
通常のGLM-Worker lifecycleへ復帰
```

固定で「5時間後」「5時間10分後」などを設定しない。

`account/rateLimits/read` から取得した5h windowの `resetsAt` を使い、
```text
wake_at = resetsAt + 小さな安全マージン
```

とする。

## wake専用セッションの処理

wake専用セッションがschedulerから呼び出されたら、原則として以下だけを行う。
```text
1. 発火済みの前回schedulerを削除
2. 親実装セッションへ短い固定指示「作業を続けろ」を送る
3. account/rateLimits/readから次回5h resetsAtを取得
4. 自セッションに次回wake schedulerを設定
5. 登録結果を確認して終了
```

schedulerを毎回追加して増殖させない。

wake専用セッションでは以下を行わない。

- 実装
- レビュー
- task判断
- diff解析
- リポジトリ全体の再読
- 状況要約
- 設計判断

親実装セッションへ送る内容も原則 `作業を続けろ` のような短い固定文とし、wake側で作業状況を再構築しない。

## wake専用セッションのトークン消費

wake専用セッションは**可能な限り最小コスト・最小トークン消費**にすること。

Luna Low等の低コスト・低reasoningモデルが利用可能なら原則それを使用する。

Sol / High reasoning等の高コストモデルをwake用途に使用しない。

モデル選択だけでなく、wake turn自体も上記5操作程度に限定し、余計な読み込み・推論・出力を行わせない。

## 親実装セッションへの通知

親実装セッションへの送信方法は、Greptile専用セッションが現在実装セッションへレビュー結果を送っている既存方式を確認し、可能ならその通信方式を再利用する。

Greptile固有のreview処理自体を共通化する必要はない。

必要なのは「別Codexセッションから既存の親実装セッションを起こす」という部分だけ。

## 手動オペレーションを残す

自動化が故障した場合でも、人間が復旧できること。

ただし人間用のscheduler管理CLIは作らない。

手動時は以下で十分。
```text
Limit確認:
  codex app-server → account/rateLimits/read
  または今回追加する読み取り専用glm-workerコマンド

scheduler確認・削除・再登録:
  Codex Desktopの既存UI

親実装セッション再開:
  Codex Desktopから実装セッションへ「作業を続けろ」
```

自動化だけが唯一の操作経路になる設計にはしない。

## 実装上の制約

- このリポジトリ固有の `IMPLEMENTATION_PLAN` やtask lifecycleをCodex autoresume機能へ組み込まない。
- wake後の作業再開処理は親実装Codexの既存手順へ任せる。
- `launchd`、cron、常駐daemonを追加しない。
- scheduler管理用の独自daemonや独自UIを作らない。
- 人間向けscheduler管理CLIを作らない。
- 不要な新frameworkを追加しない。
- Markdownだけで安定して実現できる処理をGoへ移さない。
- rate-limit JSON解析など機械的に保証すべき処理をLLMの自由解釈へ依存させない。
- 既存GLM autoresume / Greptile scheduler ownershipを壊さない。

必要なtest / scenarioを追加し、既存機能へのregressionがないことを確認する。

最終目標は、**追加クレジットを使わず、Weekly Limitへ到達するまでは親Codexの5h Limitを跨いでGLM-Workerが無人で開発を再開でき、故障時にはCodex DesktopとLimit確認手段だけで人間が復旧できる状態**とする。
`````

## Amendments

none

## Resolved references

- 「既存GLM Resume scheduler」はGLM provider側5h上限後に同じ親実装taskを起こす現行automationを指し、本taskでは変更しない。
- 「Greptile専用Codexセッション」は日次Greptile reviewを固定taskで実行し、結果を親実装taskへ送る既存構成を指す。
- 「親実装セッション」は、その時点でglm-workerを利用して開発を監督している既存Codex taskを指す。

## External feasibility

status: poc-required

## Purpose

親Codexの5h Limit中は開発全体を停止し、実reset時刻後に低コストな専用Codex taskから既存の親実装taskだけを起こして、Weekly Limitまで無人復帰できる汎用運用を成立させる。

## Contract

- 実装前に、現Codex app-serverのrateLimits取得、Codex Desktop scheduler、別taskから親taskへの送信、低コストmodel指定の実producer成立性を現在環境で確認する。
- 5h windowはprimary/secondaryの位置でなく`windowDurationMins == 300`で識別し、取得・分類が必要なら読み取り専用の薄いmachine JSON機能だけをGoへ置く。
- 5h到達時はGLMを含む開発全体を停止し、Weekly Limitや追加creditでの継続を行わない。
- wake時刻は取得した`resetsAt`へ小さな安全余裕を加えて決め、固定相対時間にしない。
- wake専用taskは旧scheduler削除、親taskへの固定短文送信、次回limit取得、次回scheduler作成、登録確認だけを行う。
- wake schedulerは1件だけを維持し、発火ごとの増殖や未処理queueを作らない。
- schedulerとtask間送信はCodex Desktop既存機能を利用し、Goへscheduler管理を実装しない。
- 手動復旧経路を維持し、既存GLM ResumeとGreptile schedulerのownershipを変更しない。

## Must not

- Weekly Limit復旧、追加credit利用、親不在中のGLM単独開発へ拡張しない。
- 同一Codex taskへ複数schedulerを置かない。
- wake taskで実装・review・task判断・diff解析・repository再読・要約・設計判断を行わない。
- scheduler操作、親task送信、独自daemon/UI、launchd、cron、人間向けscheduler CLIをglm-workerへ実装しない。
- 本repository固有Plan/task lifecycleを汎用autoresume機能へ結合しない。
- primaryを常に5hと仮定せず、rate-limit JSONをLLMの自由解釈だけへ委ねない。
- fixtureだけで外部成立性を完了扱いにしない。

## Acceptance criteria

- Original instructionの全要件を満たす。
- 実Codex app-serverから5h/Weekly windowとreset時刻をmachine-readableに識別できる。
- 実Codex Desktopで専用taskの一回scheduler、発火済みscheduler削除、親taskへの短文送信、次回scheduler登録確認が成立する。
- 5h Limit中は開発全体が停止し、reset後は親実装taskだけが起床して既存resume手順へ戻る。
- fixed 5h delay、scheduler増殖、高コストwake model、Weekly復旧、追加credit利用がない。
- 外部成立性が確認できた範囲だけをproduction contractへ進め、必要なGo変更はrate-limit取得の薄いread-only JSON境界へ限定する。
- failure時にDesktop UIとlimit確認手段で人間が復旧できる。
- 必要なscenario・regression test、独立review、必要なSol gate、親Codex最終reviewを通す。
- GLMはcommit/pushせず、完了gate後のlocal commitは親Codexが行う。pushしない。

## Historical invariants

- 最上位目的は、Sol High相当の品質を可能な限り維持しながらCodex/Sol実消費量を大幅に削減すること。
- 既存GLM Resume schedulerとGreptile日次schedulerは別ownershipであり、本taskはそれらを変更しない。

## Dependencies

none

## Review findings

none

## Current boundary

source comment absolute invariant task完了後の最優先ACTIVE。実装前に外部成立性gateを行い、fixtureだけでGo実装へ進めない。
