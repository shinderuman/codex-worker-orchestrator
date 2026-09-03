# Task: Codex command approval coverage

## Original instruction

````text
~/.codex/rules/default.rulesに
prefix_rule(pattern=["git", "commit"], decision="allow")
を足せば永続的に許可したことになるか
同様の話で

この操作は高リスクと見なされるため、明示的な承認が必要です
Shell
$`glm-parent-action prepare fix`
`出力なし`

これは

~/.codex/rules/glm-worker.rules

にすでに記載があるんじゃないの
ないんだとしてもこのリポジトリの ./install.sh を実行したら全部のコマンド実行できるようにしないとだめでしょ
一部のサブコマンドだけ使用可能にするんじゃなくて全部使えるようにしないとだめでしょ
````

## Amendments

none

## Resolved references

- current `~/.codex/rules/default.rules`には`["git", "add"]`等はあるが`["git", "commit"]` ruleがなく、`codex execpolicy check`でも`git commit -m x`はmatched rule 0件だった
- current `~/.codex/rules/glm-worker.rules`とtracked `codex/rules/glm-worker.rules`は同一で、`glm-worker`全体はallowする一方、`glm-parent-action`はsubcommand allow-listで`prepare`を除外している
- `codex execpolicy check`で`glm-parent-action prepare fix`と`prepare decision`はmatched rule 0件、`fix ... --origin codex-review`と`finalize-check go-test-race`はallow、unknown subcommandはunmatchedだった
- `install.sh`はtracked `codex/rules/glm-worker.rules` 1fileだけをmanaged manifestへ載せ、`~/.codex/rules/glm-worker.rules`へcopyする
- `glm-execution.md`は`prepare decision|fix`をsandbox内で行うため、今回のapproval表示には親Codexが不要な`require_escalated`を指定した実行誤りも含まれる

## Purpose

repositoryの通常parent workflowで必要な親`git commit`とvalidな`glm-parent-action`全subcommandを、`install後のCodex execpolicyで追加approvalなしに実行可能にする。

## External feasibility

status: not-applicable

## Contract

- install-managed Codex rulesから`git commit` prefixを永続allowし、commit option/messageごとの再承認を不要にする
- install-managed Codex rulesから`glm-parent-action` executable prefixを永続allowし、現在および将来のvalid subcommandを個別allow-listへ追加する運用を廃止する
- `glm-parent-action`自身のclosed CLI grammar、token binding、staging guard、lifecycle validationを実行安全境界として維持する
- `glm-worker`全体の既存allowを維持する
- `install.sh`実行後のmanaged ruleとtracked source一致を検証する
- `prepare`は引き続きinstruction上sandbox内実行とし、broad allowを不要なescalationの指示へ変えない

## Must not

- GLM worker/reviewerへGit commit/push authorityを付与しない
- force/non-fast-forward push、tag、remote branch操作を追加allowしない
- `--dangerously-bypass-approvals-and-sandbox`を使用しない
- valid subcommandごとのmatch列挙を別fileへ移すだけの修正にしない
- broad shell、`git`全体、任意executableをallowしない

## Acceptance criteria

- `codex execpolicy check`で`git commit -m x`がallowになる
- `codex execpolicy check`で`glm-parent-action`の全valid subcommand family（start、prepare各種、decision、fix各option、milestone操作、no-go、accept、resume、finalize-check両form）がallowになる
- unknown executableや`git push --force`は新ruleでallowされない
- `./install.sh`後の`~/.codex/rules/glm-worker.rules`とtracked sourceが一致する
- install smoke、harnesslint、独立reviewer、current snapshot validationを完了する

## Historical invariants

- execpolicy allowは実行時approvalを省くもので、repositoryのsemantic commit/push authorityを拡張しない
- 親Codexだけがcommitし、GLM worker/reviewerはcommit/pushしない

## Dependencies

none

## Review findings

none

## Current boundary

timeline retention fallbackのaccepted staged変更を親commitした直後に実行し、後続Taskのapproval interruptionを先に除去する。
