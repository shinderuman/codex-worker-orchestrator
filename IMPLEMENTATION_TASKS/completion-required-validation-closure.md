# Task: 必須検証の欠落による誤完了を防ぎ、未完了・復旧時のTask identityを維持する

## Original instruction

2026-10-27 ユーザー指示（原文）:

````text
それをTaskとして起票してコミットしてPushしておけ
優先順はお前が判断しろ
だが俺は先頭のActiveは最初に処理すべきだと思っている
````

## Amendments

2026-10-27 優先判断の範囲に関する追加指示（原文）:

````text
優先順を判断しろとは既存のタスク全部と比較してという意味だ
````

## Resolved references

- 「それ」: `macos-go-test-performance.md`で、必要とされたfull/raceのうちraceが未実施でも、完了ownerがfullのみを実行してPublication・Task完了・次Taskのmetadata昇格まで進んだ問題の恒久対策。親Codexは、事故の復旧と誤完了防止機構の恒久修正が別であり、後者は未実施と説明した。
- 同事故ではユーザーから「特例としてStateを変更することを許可する」「これだと1タスク1コミットが維持できない」「Stateを変更しないとGLMのTask IDが別になってしまうはずだ」「既存のをStateをもとに戻せ」と指示された。今回限りの手動State復旧を通常手順へ一般化せず、公開前の停止と同一Taskでの正規復旧を成立させる。
- 観測された境界は、Taskの要求・親の判断と、Worker由来の単一validation formから継承されるcandidateの`required_validations`の不一致。親が採用前に欠落を確認しなかった責任と、宣言済み集合だけを満たして完了へ進める機械契約の限界を区別する。自然言語の全要求を機械が自動理解できるとは扱わない。
- 事故の実装sourceはGit commit `6ec66e3aa9a3341f25fde46804efe8f789eeafe4`。同じTask ID `554d948b-7609-4412-bc93-55ce2ecaf5e2`のartifact `final-adoption.json`、`premature-completion-recovery/recovery.json`、`premature-completion-recovery/verified-final-state.json`と正規validation記録が、早過ぎた完了、保全付きState復旧、同一sourceへのrace後検証を保持する。artifactは既存のGLM task artifact storeから取得し、取得不能な証拠を推測で補完しない。
- 既知の入口は`glm-worker/internal/controller/publication_final_validation.go`、`publication_validation_obligation.go`、`publication_acceptance.go`、`publication_completion_readiness.go`、`glm-worker/internal/parentactioncmd/finalization.go`と既存Workflow/Stateのvalidation宣言・完了owner。実装時のcurrent sourceで責務と既存修正を再確認する。
- `canonical-publication-source-continuity.md`はcandidate/remote/source lineage、promoteと外部変更の識別、controller/runtime再入の整合を扱う。本Taskは必須検証集合の完全性と未完了・復旧中のTask identityの維持を担当し、同じ判定を別ownerへ二重実装しない。
- `validation-failure-retry-admission.md`の再実行条件・未宣言formのrouting拒否、`validation-evidence-unreadable-seal.md`の読取不能証拠、`user-requirement-ingress-binding.md`の新規user turn取り込みとは別責務。既にtracked化された要求に必要な検証が宣言から抜ける問題を、これらの完了待ちにしない。

## Purpose

必要な検証を宣言から落とすことで未実施のまま公開・Task完了へ進むことを防ぐ。未達時は同じTask ID・source・sessionの下で検証と修正を継続し、Taskの作り直しや公開後の特例State修復を通常経路で必要としない状態にする。

## External feasibility

status: not-applicable

## Contract

- tracked Taskの要求と親のsemantic判断から、必要な検証集合と適用条件を既存の正規ownerへ明示的に束縛する。親は意味上の完全性を確認し、機械はTask contract digest、source/candidate、宣言集合と実証拠の整合を強制する。自然言語の自動判定器や第二の要求正本は作らない。
- Worker報告の単一formや検証要約だけで、親が確定した必要集合を縮小しない。fullとraceの両方が必要な場合、一方しか宣言されていなければ採用・公開前に欠落を検出する。必要集合や適用条件が未確定の場合も空集合・検証不要へ推定しない。
- 必須formごとの未実施・running・FAIL・interrupted・読取不能・source不一致を成功と区別する。必要な証拠が揃うまでPublication、terminal complete、Task削除、次Task昇格へ進めない。backendごとの必須補完検証も対応する正規formの成立条件として維持する。
- 検証失敗、ユーザー中断、source/Task改訂による証拠失効から、既存handoff・checkpoint・journalの正規復旧で再入する。同じsemantic TaskのTask ID、必要なWorker/Reviewer session、未消化の検証義務を保持し、別Taskへの付け替えで欠落や過去FAILを隠さない。source改訂で必要な独立Reviewとfresh validationを省略しない。
- 親acceptは実装候補の採用とTask全体の完了を区別する。必要集合の完全性を確定する境界、集合の変更権限、各transitionの拒否条件を実装前に親が判断し、既存schema/ownerへ最小限で接続する。
- 成功時は既存のinstall・source binding・独立Review・Publication・Retireと1 Task 1 Commitを維持する。最終結果にはTask全体の完了か、残る検証待ちかを機械的に区別できる証拠を返す。

## Must not

- 指示文への注意書き、親の「確認した」という自由文、テスト関数や検証宣言の存在確認だけで恒久対策完了としない。
- 検証集合の欠落を、既に宣言されたformのPASSや、診断用testのPASSで補ったことにしない。
- 全Taskへ無条件にfull/raceを追加して完全性の問題を隠さない。検証不要・単一form・複数formの正当な要求を扱う。
- 通常経路で手動State編集、新規Task ID、履歴書換え、追加の終了commit、公開後の未達復旧を必要とする設計にしない。Complete後の再開APIを本Taskの代替解決にしない。
- 第二state DB、専用daemon、都度のwrapper、旧schema互換・推定fallback、追加AI callによる汎用分類器を追加しない。
- source/Task/runtime identity、独立Review、失敗・中断の証拠、既存の権限境界を弱めない。

## Acceptance criteria

1. 必須集合がfull/raceなのにWorker宣言がfullのみという事故条件を、正規入口から再現する。欠落時はremote、terminal complete、Task削除、次Task昇格を変更せず停止する。
2. full PASS・race未実施/running/FAIL/interrupted・source不一致をそれぞれ区別し、必要集合が全て同一の有効なcandidateに束縛されて成立した場合だけ完了する。既存backendの必須補完検証を省略できない。
3. 失敗・中断・改訂から手動State補正なしで正規再入し、同じTask IDと必要なsession・検証義務を維持する。新規Taskの開始と同一Taskの復旧を実stateで区別する。
4. 正当な検証不要・単一form・複数formを扱い、古いPASSの流用や宣言縮小によるfalse successを拒否する。自然言語の意味判断を機械保証したとは説明しない。
5. 必要な親判断、独立Review、current sourceに対する正規validation・install/smoke・Publication/Retireの接続を確認する。検証欠落の拒否と成功時の1 Task 1 Commitを、production ownerを駆動するテストで示す。

## Historical invariants

- Task要求の一次正本はtracked Original instruction / Amendments / Resolved references。親が意味を判断し、決定可能な整合検査は既存machine ownerが担う。
- 検証の宣言・実行・証拠・Task完了の同一性、失敗時のfail closed、同一Taskの継続、1 Task 1 Commitを維持する。
- 既存Taskの有効な機構や確認済みの独立Reviewを理由なく再実装・再実行しない。

## Dependencies

none
