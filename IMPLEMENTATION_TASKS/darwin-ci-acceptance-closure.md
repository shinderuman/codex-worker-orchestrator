# Task: Darwin acceptance の要求正本とCI接続を閉じる

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1509#discussion_r4479685902

** Minor** · **Functional Correctness** · **Quick win**

**Run the changed Darwin-only `qualitygate` test in this Darwin workflow.**

This PR changes `darwinSysctlInt` and `darwinSysctlUint64` in `glm-worker/internal/qualitygate/resource_samples_darwin.go`. `TestDarwinSysctlDecoding` has the `//go:build darwin` constraint. The Linux `basic` jobs therefore never compile this test. This workflow is the only macOS job, and it does not trigger on `internal/qualitygate/**`. It also runs only `./internal/observationexec`. As a result, no CI job validates the changed Darwin decoding. `IMPLEMENTATION_TASKS/darwin-tagged-acceptance-before-publication.md` records this same gap: the change was checked by hand only after publication.

To fix this, add the package to both path filters and to the test command.

<details>
<summary>🛠️ Proposed change</summary>
<dl>
<dd>
<br>

```diff
     paths:
       - 'glm-worker/internal/observationexec/**'
+      - 'glm-worker/internal/qualitygate/**'
       - 'glm-worker/internal/execfile/**'
...
-        run: go test -count=1 ./internal/observationexec
+        run: go test -count=1 ./internal/observationexec ./internal/qualitygate
```

Make the same `paths` addition in the `push` trigger.
</dd>
</dl>
</details>

Also applies to: 50-52

出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534718929

**[P2] Darwin uninstall の経路を変更しても、この受け入れテストが起動しない依存ファイルがあります。** `uninstallcmd.retireManagedOwners()` は `codexinstall.Retire`、`cliinstall.Retire`、`installlock.With` および `scripts/manage-pull-hook.sh` を直接呼びますが、これらのディレクトリ／スクリプトが `pull_request.paths` と `push.paths` にありません。これらだけを変更した PR では Linux 側のテストは実行されても、macOS の `/var/folders`・`/private/tmp` 実機 install→uninstall→reinstall を確認する唯一の workflow が丸ごとスキップされます。少なくとも `glm-worker/internal/codexinstall/**`、`glm-worker/internal/cliinstall/**`、`glm-worker/internal/installlock/**`、`scripts/manage-pull-hook.sh`、`install.sh`、Claude 設定パス解決側もトリガーに含め、両イベントの条件を揃えてください。

出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1564#pullrequestreview-5854509791

**Set `persist-credentials: false` on checkout.**

`actions/checkout` stores the job token in `.git/config` by default. This job then runs repository-controlled code: `go test`, `install-quality-tools.sh` and `tests/install_smoke.sh`. None of these steps needs git credentials after checkout. Disable credential persistence to reduce exposure. `zizmor` reports this as `artipacked`.

<details>
<summary>Proposed change</summary>
<dl>
<dd>
<br>

```diff
-      - uses: actions/checkout@v6
+      - uses: actions/checkout@v6
+        with:
+          persist-credentials: false
```
</dd>
</dl>
</details>

_Source: Linters/SAST tools_

</dd>
</dl>
````

## Amendments

none

### 2026-11-11 評価内で採用した宣言正本の補完

出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1540#pullrequestreview-5842899174

````text
**Move the request-body declaration rule into `IMPLEMENTATION_RULES.md`.**

Line 43 says a Darwin declaration given directly in the request body uses the same format. `IMPLEMENTATION_RULES.md` Lines 87-88 describe only the Task file `## Darwin acceptance` section. The README therefore states a behavior that its canonical source does not state.

The code does read request content when `RequestID` is set (`darwinAcceptanceTaskContent`). Add that statement to the rule text. Keep the README as a locator only.

Because `IMPLEMENTATION_RULES.md` is parent-managed metadata, the parent should make the edit.

As per coding guidelines, `README.md`: "READMEは利用者向け入口とcanonical locatorを示し、…変更可能なcurrent実装一覧を第二正本として保持しない".

_Source: Coding guidelines_

</dd>
</dl>
````

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1509#discussion_r4479685902) — `.github/workflows/observation-darwin.yml`
- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534718929) — `.github/workflows/uninstall-darwin.yml`
- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1564#pullrequestreview-5854509791) — `.github/workflows/uninstall-darwin.yml`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

qualitygate専用変更とuninstall依存owner変更が唯一のDarwin workflowをskipする。checkout credentialsも不要に保持する。

## External feasibility

status: not-applicable

## Contract

- request-backed taskのDarwin宣言についても正規の要求sourceをRulesへ明記し、READMEはそのlocatorに限定する。既存実装が読むrequest本文の見出し・形式と照合する。
- observation workflowの両event pathと実test対象へqualitygateを追加する。uninstall workflowの両eventへcodexinstall/cliinstall/installlock/claudesettings、install.shとpull-hook等の直接依存を閉じる。対象checkoutのpersist-credentialsはfalseにする。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- Task fileとcanonical request本文の宣言経路が規則・実装・利用者入口で矛盾せず、READMEが第二の規則正本を持たない。
- qualitygate専用、各uninstall依存専用の変更が対象workflowへ届くことを確認する。Darwin sysctl test、/var/foldersと/private/tmp実smoke、通常失敗の集約伝播を維持し、path listとtest commandの対応を確認する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none

## Darwin acceptance

- `./internal/qualitygate:TestDarwinSysctlDecoding`
