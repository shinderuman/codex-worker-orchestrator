# Task: PoC/observation terminal Go/No-Go packetの自己矛盾を解消する

## Original instruction

````text
TASK

現在の `2750ec82-8d56-47b9-b8f5-972d44dab43e` で、PoC/observationのterminal Go/No-Go変換に決定論的なpacket contract違反を確認した。

## Evidence

- `glm-parent-action start` が最終的に以下で失敗した。
  - `field evidenceに改行を含められません: 複数事項は同じvalue内でセミコロン区切りにしてください`
- `pocGoNoGoResult()` は `Summary` / `Evidence` / `TestObligations` の長文を `boundedText(..., packet.MaxFieldBytes)` に通す。
- current `boundedText()` は切詰め時に `"[前方を省略]\n"` をprefixとして付加する。
- その結果を直後の `packet.ValidateWorkerResult()` がfield内改行禁止として拒否する。
- 実際のDogfoodでもこの経路でterminal変換が失敗した。

## Scope

このwrapper生成packetの自己矛盾を独立taskとして起票する。

既存 `packet-presubmit-byte-validation.md` は、model producerが生成するpacketのUTF-8 byte上限違反とresult-correction削減を扱うため、本件とは別責務とする。本件をそこへ混在させない。

要求:

- wrapperが生成するPoC/observation Go/No-Go packetは、生成直後の現行packet validatorを必ず通過すること。
- `boundedText`の他利用箇所も確認し、改行禁止fieldへ同じ不正な値を生成する経路がないか範囲を確定する。
- UTF-8 byte上限を維持する。
- packet fieldの改行禁止contractを弱めない。
- 単にvalidatorを迂回しない。
- 意味を失う無条件truncateへ広げない。
- 必要十分なregression testを追加する。
- 追加model callや新しいstate/parserは作らない。

再現testでは、1536 bytesを超えるPoC worker resultから生成されたGo/No-Go resultが、

1. field byte上限内
2. field内改行なし
3. `packet.ValidateWorkerResult()` PASS
4. 必要なGo/No-Go意味情報を保持\
   することを確認する。

## Plan ordering

現在ACTIVEの `glm-flash-reviewer-routing.md` はまだ未着手で、かつ `External feasibility: observation` のため同じ壊れたGo/No-Go経路を踏み得る。

本bug taskを次に実行するACTIVEへ昇格し、現在の `glm-flash-reviewer-routing.md` は未着手状態のままNEXT先頭へ戻す。\
その他のNEXT順序は維持する。

今回はtask起票・Plan順序変更・必要なparent-managed metadata同期だけ行う。\
**バグ修正実装は開始せず停止する。**
````

## Amendments

none

## Resolved references

- task `2750ec82-8d56-47b9-b8f5-972d44dab43e`は完了済みの`authorization-context-inconsistency` observationであり、terminal変換失敗と親によるstate resetの証跡は`IMPLEMENTATION_HISTORY.md`の「2026-09-01 許可済み操作の承認判定不整合」を参照する。
- `packet-presubmit-byte-validation.md`はmodel producerのpacket byte上限違反とresult-correction削減を扱う別taskであり、本taskのwrapper生成packet責務を含めない。
- 「必要なGo/No-Go意味情報」は、PoC/observation結果を親がGo・No-Go・観測継続から判断するためのstatus、decision、evidence、options、recommendation、test obligationsと対象を指す。各fieldの表現方法は現行packet schemaを維持する。

## Purpose

wrapper自身が生成するPoC/observation terminal Go/No-Go packetについて、UTF-8 byte上限とfield内改行禁止を同時に満たし、生成直後の現行validatorを決定論的に通過させる。

## External feasibility

status: not-applicable

## Contract

- `pocGoNoGoResult()`から生成される全fieldを現行packet schemaとvalidatorへ照合し、1536 bytes超のworker resultでもbyte上限・改行禁止・必須field・Go/No-Go意味情報を維持する。
- `boundedText`の全利用箇所を確認し、改行禁止fieldへ切詰めprefix由来の改行を生成し得る経路と、改行を許容する別用途がある場合の境界を確定する。共有helperを変更するか利用側を分けるかは、他callsiteの意味保持と回帰riskを根拠に選ぶ。
- UTF-8文字列をbyte上限で処理するとき、不正UTF-8や上限超過を生成しない。短い値の意味・表現を不要に変更せず、長い値は省略された事実と親判断に必要な情報を保持する。
- wrapper生成結果を`packet.ValidateWorkerResult()`へ通す現行検証境界を維持し、validator通過を実装とregression testのpostconditionにする。
- 1536 bytesを超えるPoC worker resultからのGo/No-Go変換について、field byte上限、field内改行なし、validator PASS、必要な意味情報保持をproduction経路に対応するtestで固定する。

## Must not

- `packet-presubmit-byte-validation.md`が扱うmodel producer packetの事前byte検証・result-correction削減を本taskへ混在させない。
- packet fieldの改行禁止contract、UTF-8 byte上限、必須field検証を弱めない。
- validatorをskip・迂回したり、生成後のvalidation errorを成功扱いにしない。
- 意味情報や省略事実を区別しない無条件truncateを全callsiteへ広げない。
- 追加model call、新しいstate、parser、互換layerを追加しない。
- routing評価taskの調査・実装を開始しない。

## Acceptance criteria

- wrapper生成のPoC/observation Go/No-Go packetが、1536 bytes超の入力を含め、生成直後の`packet.ValidateWorkerResult()`をPASSする。
- 再現testが各対象fieldのUTF-8 byte上限内、field内改行なし、validator PASS、親Go/No-Go判断に必要な意味情報保持を確認する。
- `boundedText`の全利用箇所を確認し、同じ改行禁止違反を生成し得る範囲を実装またはtestで閉じている。対象外callsiteは理由を説明できる。
- 短い入力、UTF-8 multibyte境界、長い入力の省略表示について必要十分な回帰確認があり、packet contractを弱めていない。
- 追加model call、新state/parser、model producer向けpresubmit責務、routing変更を導入していない。

## Historical invariants

- 現行packet validatorをmachine result authorityとして維持し、field内改行禁止、UTF-8 byte上限、status別必須fieldを保持する。
- PoC/observationはproduction diffなしで親Go/No-Goへ戻し、GLMだけでimplementationへ昇格させない。
- 親Codexの意味判断、独立review、validation authority、parent-managed metadata guard、GLMのcommit/push禁止を維持する。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。Dogfood task `2750ec82-8d56-47b9-b8f5-972d44dab43e`で決定論的再現と原因箇所を確認済み。task起票とPlan順序変更だけを行い、source変更、test追加、GLM callは開始していない。
