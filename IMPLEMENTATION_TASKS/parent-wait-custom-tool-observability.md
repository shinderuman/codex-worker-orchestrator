# Task: parent wait custom-tool observability

## Original instruction

````text
目的:

* Sol High相当の品質を維持しながらCodex tokenを減らす
* correctnessを捕捉する意味判断reviewは削らない
* same-state / same-evidence waste、重複読取、不要なparent再入を見つける
* correctness defectはQuality Delta findingとして扱う

finding確定前に、open / closed Issue、current code、Plan / Taskを確認し、同じroot causeを重複管理しない。

findingは次へ処理する。

* 新規実装finding → 新しい`priority:do` Issueを作らずTask化
````

## Amendments

none

## Resolved references

- Dogfood bundle `2352c5a5-66b2-4263-8f4a-40e0d5d6e5c8` の `analysis-index.json` は `parent_wait_calls.count=1` を返した
- 同bundleのparent rolloutではtask execution中に、current Codex Desktopの `custom_tool_call` / `name=exec` wrapperから `tools.write_stdin` を呼ぶwait requestが多数存在する。07:08〜08:09だけでも300000ms requestが14回以上あり、task開始直後には30000ms requestも複数ある
- current `glm-worker/internal/app/bundle_analysis.go` のwait scannerは `function_call` かつ `name == "wait"` だけを `scan.waits` へ記録し、`custom_tool_call` のtool activity自体は認識していてもinner `tools.write_stdin` をwait evidenceとして認識しない
- closed #140 / #455はhealthy waitのruntime ownershipとhost boundaryを扱う。今回のFindingはその挙動を再実装するものではなく、既に発生したparent wait/re-entryをbundle measurementが欠落させる独立したobservability correctness defectである

## Purpose

current Codex rollout transportで実際に発生したparent wait requestを `analysis-index.json` のwait evidenceへlosslessかつboundedに反映し、Codex Reduction監査がparent re-entryを過少計上しないようにする。

## External feasibility

status: not-applicable

## Contract

- `parent_wait_calls` はlegacy `function_call name=wait` と、current Codex Desktopがcanonical custom-tool transportで発行する `tools.write_stdin` waitの双方を認識する
- current `custom_tool_call name=exec` のうち、machine-generated canonical wrapperとして一意に識別できる `tools.write_stdin` requestだけをwaitとして採用する。generic shell / JavaScript本文、自然言語、単なる文字列出現からwaitを推測しない
- custom-tool waitから `call_id`、request locator、requested yieldを可能な範囲で構造化し、legacy waitと同じ意味のfieldへ正規化する
- wrapper metadataとinner request等に矛盾がある場合は値を推測せずunknown/conflictとしてfail closedする
- wait output / duplicate requestの対応付けは既存call identityを再利用し、同じrequestをlegacy/current transportの両方として二重計上しない
- current transport追加後も既存legacy waitのcount / yield classification / duplicate detection semanticsを維持する
- bundle生成はread-onlyのままとし、model callやruntime task stateを追加しない

## Must not

- 任意JavaScriptを実行・完全parseしてwaitかどうかを判定しない
- `tools.write_stdin`という文字列を含むだけのunrelated custom tool inputをwaitとして数えない
- malformed/矛盾したrequestからyield時間を補完推定しない
- #140 / #455のhost wait ownershipやblocking policyを本taskへ再実装しない
- parent waitを減らすためにsemantic review、user interruption、real lifecycle transitionを隠さない

## Acceptance criteria

- real bundleと同型の `custom_tool_call name=exec` + canonical `tools.write_stdin` fixtureで、30000ms / 300000ms waitが `parent_wait_calls` に計上される
- ordinary `tools.exec_command`、`tools.write_stdin`文字列をデータとして含むだけのcustom call、shape不一致requestをfalse positiveにしない
- legacy `function_call name=wait` fixtureの既存count / yield class / return pairingが不変である
- legacy/current transportが混在するfixtureでrequest数を二重計上せず、call identityとlocatorが監査可能である
- malformedまたはyield evidence conflict fixtureが推測値を返さずboundedなunknown/conflictになる
- bundle `2352c5a5-66b2-4263-8f4a-40e0d5d6e5c8` 相当のrolloutを再現する回帰fixtureで、`count=1`の過少計上が再発しない
- Repository Lintと関連Go testがPASSする

## Historical invariants

- bundle / telemetryは元evidenceを改変しないread-only projectionである
- parent waitのobservabilityはCodex Reduction判断用であり、semantic correctness reviewを削減対象へ変えない
- host schedulerがrepositoryから強制不能な部分は#455のexternal boundaryとして維持する

## Dependencies

none
