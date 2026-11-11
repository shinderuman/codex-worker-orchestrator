# Task: staging response の符号化後byte上限と即時失敗

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607603

** Minor** · **Stability & Availability** · **Quick win**

**Fail fast when a staging response is oversized.**

`readStagingResponse` returns `false` for these cases:

- The response file is missing.
- The response file is partially written.
- The response file exceeds `stagingMaxResponseBytes`.

`awaitResponse` then polls until the 65-minute `StagingClientTimeoutDefault` expires. The oversize case is deterministic and is not a pending state.

The executor caps stdout and stderr at 16 MiB each. `json.Marshal` escapes `<`, `>`, and `&` as `\u003c` and similar six-byte sequences. Go test output often contains `<nil>`, `->`, and `&{`. When both streams reach the cap with this content, the encoded file can exceed 64 MiB. The worker then waits more than an hour and receives no diagnostic.

Use separate results for "not yet complete" and "oversized" so the client can stop immediately on oversize. Also disable HTML escaping on the writer side. See the comment on `docker_test_executor.go` `writeStagingResponse`.

<details>
<summary>🐛 Proposed fix</summary>
<dl>
<dd>

```diff
-	data, err := io.ReadAll(io.LimitReader(file, stagingMaxResponseBytes+1))
-	if err != nil || len(data) > stagingMaxResponseBytes {
-		return StagingResponse{}, false
-	}
+	data, err := io.ReadAll(io.LimitReader(file, stagingMaxResponseBytes+1))
+	if err != nil {
+		return StagingResponse{}, false
+	}
+	if len(data) > stagingMaxResponseBytes {
+		return StagingResponse{SchemaVersion: StagingSchemaVersion, Token: token, ExitCode: 1, Failure: "staging responseがsize上限を超えました"}, true
+	}
```
</dd>
</dl>
</details>

出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607638

** Minor** · **Stability & Availability** · **Quick win**

**Disable HTML escaping when encoding staging responses.**

`json.Marshal` escapes `<`, `>`, and `&` as six-byte `\uXXXX` sequences. Go test output contains these characters often. With the 16 MiB stdout and stderr caps, the encoded response can exceed the client limit of 64 MiB. The client then waits until its deadline. See the comment on `staging.go` `readStagingResponse`.

<details>
<summary>🐛 Proposed fix</summary>
<dl>
<dd>

```diff
-	data, err := json.Marshal(response)
-	if err != nil {
-		return err
-	}
+	var encoded bytes.Buffer
+	encoder := json.NewEncoder(&encoded)
+	encoder.SetEscapeHTML(false)
+	if err := encoder.Encode(response); err != nil {
+		return err
+	}
+	data := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
```
Add `"bytes"` to the imports.
</dd>
</dl>
</details>

出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534997589

**[P2] owner が正常に生成した staging response をサイズ超過として永久に読み飛ばします。** owner 側 `docker_test_executor.go` は `stdout` と `stderr` を**各16 MiB**まで許容してから `json.Marshal(response)` しますが、JSONエスケープによる増幅を考慮していません。例えば両方が16 MiBの `\\` で埋まるだけで、シリアライズ後はそれぞれ約32 MiB、合計64 MiB＋JSONヘッダとなります。あるいは一方が12 MiBの制御文字 `\x00` で埋まると `\u0000` へ6倍に展開して72 MiBになります。`readStagingResponse` は `stagingMaxResponseBytes=64 MiB` 超を常に `(false)` とし、request 自体は実行済み・response も存在するのに最大65分 timeout まで待ち続けます。実際のシリアライズ後の byte budget を owner 側で強制し、超過時は確実に読み取れる小さな failure response を emit してください（client 限度と整合する単一の契約が必要です）。
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607603) — `glm-worker/internal/dockertest/staging.go`
- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607638) — `glm-worker/internal/runner/docker_test_executor.go`
- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534997589) — `glm-worker/internal/dockertest/staging.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

HTML escapeを止めるだけではbackslashや制御文字の増幅を解消できず、owner生成responseがclient上限を超えて65分待機する。

## External feasibility

status: not-applicable

## Contract

- 実serialization後のbyte budgetをowner/client共通契約にし、超過時は同tokenに束縛した小さいfailure responseへ収束させる。clientは未到着/部分書込みと確定超過を分けて即時失敗し、未検証tokenの成功responseを捏造しない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- stdout/stderr上限近くの<>&、backslash、制御文字と境界値を検証する。超過が長時間pollにならず、欠落/部分responseの待機・取消・token不一致拒否・既存response非置換を維持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
