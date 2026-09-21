# Task: test impact selectionとtest suite retirement

## Original instruction

````text
# 14. BLOCKED / USER PERMISSION WAITとして保持するtask

以下はtask fileを作ってよいが、`blocked-user-permission`とし自動開始しない。

---

## Blocked D: test impactによるtest省略

品質証拠後。
````

## Amendments

- 2026-08-22 parent maintenance:

````text
## 11. blocked taskはplaceholder contractのままACTIVE化しない

blocked taskには、

> 許可後の個別contract

だけが書かれているものがあります。

現在blockedである間はそれで構いません。

ただしユーザー許可が出た時に、そのままACTIVEへ昇格して実装開始しないでください。

まず、

1. ユーザー許可をAmendmentへlossless保存
2. prerequisite evaluation artifactを読む
3. concrete Contract
4. Must not
5. Acceptance criteria

をtask fileへ確定する。

その後でACTIVE候補にしてください。

「permission received」だけで設計未確定taskをGLMへ投げないでください。
````

- 2026-09-22 user requirement:

````text
後方互換性と同じ話でこのリポジトリに回帰テストってどれぐらいある
回帰テスト自体はあってもいいが、永久に残り続けるのもどうかと思う
````

````text
104のタスクにマージすればいいと思う
````

## Resolved references

- 2026-09-22の「104」は `IMPLEMENTATION_TASKS/104-test-impact-selection.md` を指す。
- 今回の追加要求は新規独立taskではなく、このtaskのtest portfolio最適化責務へ統合する。

## Purpose

verification cost削減可能性を安全に採否するとともに、historical regression testが不要な構造・旧実装詳細を永久に固定し続けないtest portfolioを維持する。

## Contract

Task 014のevidenceとユーザー許可に基づき、ACTIVE候補化前に次の2系統を同じtest portfolio評価として具体化する。

- test impact selection: 変更影響と品質証拠に基づき、各validation runで安全に省略可能なtestがあるか評価する。test自体のretirementとは分離して判断する。
- test suite retirement / consolidation: 既存testを棚卸しし、historical regression sentinel、current contract/invariant test、恒久的なmachine guardを区別する。過去事故専用testは、現在のbehavior/invariantへ一般化できる場合は通常のcontract/invariant testへ統合し、原因構造が消滅して同等以上の上位guardで保証済みならretire候補とする。現在も有効なinvariantを直接保証するtest/guardは保持する。

retirement / consolidation判断は「regressionと呼ばれているか」ではなく、現在保証すべきbehavior、重複coverage、root causeとなった構造の存否、上位guardの有無、escaped defect riskを根拠にする。historical fixtureや旧implementation detailをtest側へ独立した互換層のように残さない。

## Must not

- 品質証拠なしにtestを削らない。
- `regression`という名称、古さ、過去バグ由来という理由だけでtestを削除しない。
- current invariant / contract coverageを失うretirementやselectionを行わない。
- test実行の省略とtest asset自体の削除を同一判断として扱わない。
- productionに存在しない旧policy / state machine / parser / compatibility behaviorをtest側で永久保存することを、回帰防止だけを理由に正当化しない。

## Acceptance criteria

ユーザー許可原文をAmendmentsへ保存し、Task 014 artifactを読んだうえで、ACTIVE候補化前に以下を含むconcrete Contract / Must not / Acceptance criteria / rollbackを確定する。

- suite-level coverageとfailure evidenceに基づくtest impact selection条件。
- historical regression sentinel、current contract/invariant test、machine guardの分類方法。
- regression testを保持、一般化・統合、retireする各条件と必要証拠。
- retirement後も現在のbehavior/invariant coverageが同等以上であることの確認方法。
- test asset削減とper-run test omissionを独立にrollbackできる境界。

## Historical invariants

full test gate。

Task 014は完了済み。既存event/telemetry/roundではtest call数・duration・failure outcomeまで測定できるがsuite-level coverageとper-suite failure / escaped contrastはunknownで、omission candidateは提示されなかった。このevidence不足をtest省略の根拠にしない。

回帰防止は過去事故の具体形を永久保存すること自体を目的にしない。現在も有効なbehavior/invariantを直接保証することを目的とし、恒久原則に昇格したguardはその原則が有効な限り保持できる。

## Dependencies

none
