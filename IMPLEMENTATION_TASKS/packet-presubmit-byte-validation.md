# Task: packetのUTF-8 byte上限違反による追加model callを減らす

## Original instruction

````text
2,4,5,6は起票していい
作業順は任せる
````

## Amendments

none

## Resolved references

- 「4」は2026-09-01のCodex・GLM統合分析で提示した次の案を指す。承認対象は案2・4・5・6だけであり、本taskは案4の独立責務を扱う。今回の指示は起票と順序決定で、実装開始ではない。
- 共通原証跡はbundle `70028e7d-ef53-4f70-8c7e-765d7fb78ee0.zip`（2026-09-01 05:22頃採取、SHA-256 `6aa50833970f9a44d5ed12c89f47fddd237e34c7904a22d8391c87b5abbfffe7`）。保存先は`/Users/shinderumanm/.glm-worker/exports/4b1083bd6f6e13220f3e0d653377d694f010b8951c788559f19840a14a0df6d0/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.zip`。以下の原証跡pathはbundle内相対pathであり、task telemetryは`task/telemetry/70028e7d-ef53-4f70-8c7e-765d7fb78ee0.jsonl`を指す。
- 承認された案の本文を次に保存する。分析時点の観測であり、current実装との差異は着手時に確認する。

````text
### 4. packetのUTF-8 byte上限違反による追加model callを減らす

優先度: 中。Codex・GLM双方の候補。

- 問題: 初回response全体5801 bytesは6KiB以内だったが、summary1720 bytesとrequirement_coverage1544 bytesが各1536 bytesを超えた。結果修正専用callが71.513秒・3 turns発生した。
- 証拠: task telemetry L1〜2。修正call `fd9b7eb4-8f0d-4851-b60c-109e33a859e8`、`retry_reason: invalid-packet-result-correction`。
- 範囲: 現行validatorを再利用する提出前検証と、詳細を既存artifactへ退避する経路を検討する。文字数とUTF-8 bytesを区別する。
- 期待効果: 形式修正だけの別callを減らす。ただし修正に必要な生成tokenや時間がゼロになるとはしない。
- 検証: 日本語の境界値、field上限、全体上限、必須field、artifact参照、意味保持を確認する。上限撤廃・自動切捨てで通さない。
- 成立性: GLM案の`StructuredOutput` handler改修は、外部CLIが持つtoolをrepositoryから変更できるか未確認。正式な変更面が確認できるまではその実装方式を確定しない。
````

## Purpose

提出後の形式修正専用model callを減らすため、現行packet validatorと既存artifactを使った提出前検証の成立性を確認し、採用可能な経路を実装する。

## External feasibility

status: observation
assumption: 外部CLIを非公式に改変せず、実modelのpacket提出前に現行validatorで検証し、その結果を同一call内で修正へ使える正式な経路が利用できること。

## Contract

- 初期段階はread-onlyの成立性確認とする。既存validatorの利用可能な入口と実producerの提出経路を確認し、外部CLI所有のStructuredOutput handlerをrepositoryから変更できるとは仮定しない。
- 実producerの日本語field上限超過を検出し、同一call内で修正または既存artifactへの詳細退避を行った後、現行validatorが受理するterminal outcomeまでを最小の意味的成功条件とする。固定の長時間観測や評価専用model callを無断追加せず、取得可能な証拠と不足を示す。
- 親Codexが成立性のGo/No-Goと変更責務を判断するまでproduction実装しない。Go後は親がExternal feasibility宣言を観測証拠付きで更新し、同一taskの実装段階へ進める。No-Goはblockerと未達の実装要求を明示し、観測完了だけで修正完了としない。
- 採用する経路は現行validatorを再利用し、文字数ではなくUTF-8 bytesでfield・全体上限と必須fieldを確認する。詳細を既存artifactへ退避する場合も、packetの意味・必要な判断材料・辿れる参照を保持する。

## Must not

- 上限撤廃、自動切捨て、必須field削除、情報を失う圧縮で通さない。
- 外部CLIの非公式改変や未確認のtool hookへproduction設計を依存させない。人工fixtureの成功だけを実producerの成立性証拠としない。
- 独自validatorとの二重管理、新しい永続state、追加の評価専用model callを導入しない。形式修正のtoken・時間がゼロになるとは断定しない。

## Acceptance criteria

- 正式な提出前検証面の可否を実producerの証拠と不足情報から親Codexが判断している。
- Goの場合、日本語UTF-8の境界値、field/全体上限、必須field、artifact参照、意味保持の検証と独立reviewを通過している。
- 採用経路で防げる上限違反が提出前に判別され、別callによる形式修正の削減を確認できる。観測できない効果は未検証として報告する。

## Historical invariants

- 親Codexの意味判断・最終採否、独立review、snapshotに対応したvalidation authority、parent-managed metadata guard、GLMのcommit/push禁止を維持する。
- 最上位評価はCodex ReductionとQuality Deltaとし、GLM token削減だけを採用理由にしない。

## Dependencies

none。Planの優先順はhard dependencyを意味しない。

## Review findings

none

## Current boundary

未着手。2026-09-01のユーザー承認により起票した。分析時点の候補と証拠を保存しただけで、調査・実装・新しいmodel callは開始していない。
