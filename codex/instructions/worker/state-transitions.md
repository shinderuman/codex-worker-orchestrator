# 永続状態の状態遷移品質ゲート
永続状態・設定・cache・manifest・sidecar・local fileに関わる変更は、意味変更の意図の有無に関わらず時間軸上の意味ある状態遷移を選定し検証する。全変更で全遷移を機械実行せず、この変更が実際に影響する遷移だけを選び、選定理由と検証結果を確認する。高リスク扱いは永続状態の意味変更・current schemaの拒否条件・rollback/recovery・既存ユーザーデータ破壊可能性で意味判断が必要な場合だけに限定する。

## 遷移の選定
変更内容に応じて意味あるものだけ選ぶ。
- fresh導入 / current環境への再実行
- 追加・変更・削除、有効化・無効化、override適用・override解除
- current state / 欠損 / 破損 / 部分消失
- unsupported old stateのreject / skip / reset / rebuild / delete / non-resumable境界
- 途中失敗、rollback・retry・resume・recovery
- 永続識別子・pathの変更
- 既存ユーザーデータ・設定の保持

## 設定・override・managed fileの独立観点
- 適用後に変更前の意味的状態へ戻れるか。追加物がoverride解除や削除後に残留しないか。
- 削除した値が親環境・別経路から再流入しないか。
- null・空・欠損入力が黙って無視(silent no-op)されず、意味に応じてfailしtargetを書き換えないか。
- baselineを現在値(変更後)から誤再構築していないか。欠損・部分消失stateを安易に安全復旧扱いしないか。
- unsupported old stateをcurrent stateへ変換・昇格・推定せず、current contractだけを正規入力として扱うか。

## 永続識別子の変更
current contractで必要な識別子だけを維持する。旧識別子のalias・dual read/write・自動変換を追加せず、unsupported stateは用途に応じてreject / skip / reset / rebuild / delete / non-resumableとする。既存ユーザー所有データの破壊防止はcompatibilityとは分離して検証する。

## recovery手順
README・error文のrecovery手順は概念上またはtestで検証する。未検証のものを安全と表現しない。

## PACKET報告
状態遷移の結果(変更前後・unsupported state・disable/remove・recovery・data保護懸念)は新fieldを増やさず既存field(`SUMMARY`・`INVARIANTS`・`UNVERIFIED`・`RESIDUAL_RISK`等)へ短く圧縮する。schema拡張・packet長大化は本当に必要な場合だけ。
