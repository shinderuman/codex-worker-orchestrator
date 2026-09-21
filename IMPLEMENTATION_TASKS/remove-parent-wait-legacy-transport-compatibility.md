# Task: remove parent wait legacy transport compatibility

## Original instruction

```text
また後方互換性作られてないかソースチェックしてくれ
IMPLEMENTATION_PLANに追加しておいてくれ
```

## Amendments

none

## Resolved references

- current main `bf4088a5f43b7e4d9fbc7a247f4f141f905a8558` の parent wait bundle analysis は、current `custom_tool_call name=exec` / `tools.write_stdin` waitを既存 `function_call name=wait` 表現へ正規化する。
- 同実装のtestは legacy `function_call name=wait` の受理維持と、legacy/current transport混在入力のdedupを明示的に固定している。
- commit `bf4088a5f43b7e4d9fbc7a247f4f141f905a8558` は `preserve legacy wait count/yield/duplicate semantics` を変更意図として明記している。
- `IMPLEMENTATION_RULES.md` のmachine-only data原則は、旧parser / migration / fallback / version bridge / dual protocolを「一応読める」ために恒久追加しない方針を正とする。
- forward-only compatibility gateは旧schema promotion等を検出するが、このtransport dual-readは現在の検出境界をすり抜けている。

## Purpose

parent wait bundle analysisへ再導入されたlegacy/current dual-transport compatibilityを除去し、current transportだけをcanonical inputとして扱う状態へ戻す。

## Contract

- parent wait analysisのcanonical transportはcurrent `custom_tool_call name=exec` / `tools.write_stdin` とする。
- legacy `function_call name=wait` をcurrent transportと並列に受理・維持するcompatibility behaviorを除去する。
- legacy/current混在入力を正式対応として維持するtest contractを除去する。
- current canonical custom-tool waitのcount / yield classification / return pairing / duplicate detectionは維持する。
- forward-only compatibility gateの検出境界も確認し、同種のtransport-level compatibility再導入を機械的に防げるなら最小scopeで補強する。

## Must not

- legacy transportを別名・別wrapper・別normalization layerで残さない。
- current custom-tool wait observabilityを削除しない。
- bundle evidenceの意味を変えてparent waitを過少計上しない。
- unrelated compatibility cleanupを本taskへ混ぜない。

## Acceptance criteria

- current canonical `custom_tool_call name=exec` / `tools.write_stdin` waitが従来どおり観測される。
- legacy `function_call name=wait` はcurrent canonical inputとして扱われない。
- legacy/current mixed-transport compatibilityを要求するtestが残らない。
- 同種のlegacy/current dual-transport compatibilityがforward-only policyをすり抜けないことを、既存または追加のmachine gate / regression testで確認できる。
- Repository Lintと関連testがPASSする。

## Historical invariants

- machine-only state/schema/transportはforward-onlyを正とし、active task保護と恒久互換を混同しない。
- bundle / telemetryは観測対象のevidenceを過少計上しない。

## Dependencies

none
