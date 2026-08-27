# テスト規則
- 変更したbehaviorを直接保証するテストを同じ作業単位で追加・修正・実行する。
- bug fixでは、再発防止に有効なら最小のregression testを追加する。
- 正常系に加え、実際に成立し得る主要な失敗境界・境界値だけを確認する。全分岐・全異常系の網羅自体を目的にしない。
- implementation detail、関数名、呼出順序、test runnerの内部動作、Markdownやpromptの自然言語本文をcontractとしてpinしない。
- test専用にproduction相当のstate machine・policy・parserを再実装しない。production behaviorを直接呼んで検証する。
- scenario・fixture・test helper自体を検証するためだけのtestを増やさない。
- 既存testと実質同一のcaseを追加する前に統合し、自然に表現できる場合はtable-driven testを使う。
- coverageは未検証の重要behaviorを探す診断材料として使い、coverage率や未実行branchを埋めること自体を完了条件にしない。
- 「全部のテスト」「未テスト範囲なし」等がユーザー要求として明示された場合だけ、その要求範囲を勝手に狭めない。
