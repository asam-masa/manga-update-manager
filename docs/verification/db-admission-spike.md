# DB停止と実行開始の共通制御Spike

Issue #137。2026-10-18、Windows・Go 1.26.9・modernc.org/sqlite v1.60.1・CGO無効。一時SQLiteだけを使い、候補1方式を検証した。本番Store・Application・UIは変更しない。

## 問いと候補

[承認済み方針](../cover-persistence.md#db停止時の待機中実行中処理)を、Store内に置く共通制御で満たせるかを調べる。Applicationの呼び出し元だけで制御する方式は、別経路や接続待ち後の検査漏れを防ぎにくい。本候補は全操作の移行を必要とするが、境界を一か所へ集められる。

`tools/spikes/db-admission/admission_test.go`に試作を閉じ込め、本番へそのまま転用しない。

1. 停止通知かcontextキャンセルを待てる直列枠を確保する。
2. `sql.DB.Conn`で接続を取得する。ここではまだ実行開始としない。
3. 停止と同じmutex内で停止・キャンセルを再確認し、実行権を確定する。
4. mutex外でDB処理・結果消費・必要なTx後始末を行う。処理自身から停止通知できる。
5. 接続を返してから直列枠を解放する。返却エラーは保持して停止するが、この失敗経路の故障注入は未検証。

開始とは3の実行権確定であり、SQL呼び出しの瞬間ではない。開始が先なら、その後に停止しても処理を完了できる。コールバックは一つのDB操作と必要な後始末だけを含む契約で、無関係な次のDB操作を同じ実行権で継続することは許可しない。試作は内部順序まで機械的に検査しない。

## 検証結果

| ケース | 確認した結果 |
| --- | --- |
| INSERT後のTxを保持して停止 | 開始済みのCommit／Rollbackは完了し、観測SQLの行数1／0。並行要求と停止後の読取・書込はコールバック未実行 |
| 別Connで単一接続poolを占有 | `DB.Stats().WaitCount`でpool待ちを確認。停止後に接続を返しても実行を拒否 |
| 直列枠待ち・pool待ちでcontextキャンセル | キャンセルを返し、枠が漏れず後の正常INSERTが成功 |
| 停止と開始を同時に解放（1試行100回） | mutex内で停止済みなら開始しない。開始できた処理は正常完了し、停止後の再受付は拒否 |

上記を20回反復して成功した。同期はchannelとpool待ちの統計確認で行い、固定sleepを成立条件にしない。各fixtureの15秒contextとテスト全体のtimeoutは試験のハング検出用で、本番の強制中断方針ではない。

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./tools/spikes/db-admission -count=20 -timeout=2m
```

## 限界と採用前の条件

- 健全な一時DBで停止を明示する試験で、安全性不明を検出する実装ではない。Commit結果不明の印だけでDB停止を決める方式も採用しない。
- `Conn.Close`はpoolへの返却であり、物理接続の破棄・隔離の証明ではない。ゲート経由の再利用だけを止め、pool内部の保守や直接アクセスは制御しない。接続隔離・Close/Rollback失敗・故障後の再利用条件は別途検証する。
- pool待ち中の停止は接続取得自体をキャンセルしない。拒否は取得が戻った後で、contextが先に終了すればそのエラーを返す。即時応答や無期限待ちの解消は保証しない。
- 本番移行ではRows消費・Close、TxのCommit/Rollback、結果と後始末のエラー保持まで実行権内へ閉じ込める。SQL・接続を外へ漏らさず全Store操作が経由することを検証する。再入は禁止し、初期化・終了との順序も定める。
- 観測用の直接SQLは試験外部からの状態検査で、停止後の本番読取や自動回復を許可する契約ではない。
- 全スケジュールの証明、race検出、実I/O故障、既存Store互換性、Wails/UI、画像受付連動は対象外。本番採用は未確定で、ADR-0013はPendingを維持する。

## 一次資料

- [Go database/sql DB.Conn](https://pkg.go.dev/database/sql#DB.Conn)：取得のcontextと返却契約。
- [Go database/sql Conn.Close](https://pkg.go.dev/database/sql#Conn.Close)：poolへの返却と物理隔離を分ける。
