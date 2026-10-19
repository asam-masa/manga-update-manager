# 現在画像の限定HTTP配信

Issue #163時点の記録。正本は[画像保存・配信契約](../cover-persistence.md#ファイルと配信の安全条件)と[ADR-0013](../adr/0013-define-cover-operation-boundaries.md)。本番MiddlewareとAppへ接続した。後続#165の[画像表示とexe確認](./display-current-cover.md)が本番表示の現在の範囲であり、画像設定UIは未実装である。

## 経路と寿命

`GET /__covers/works/<作品ID>`だけを扱う。作品IDは先頭ゼロ・符号なしの正のint64十進表記とし、非正規パス・エンコード・余分なパス・空クエリを含むクエリを404、GET以外を405で拒否する。予約名前空間外は既存アセットへ渡す。正常応答はimage/png・no-store・nosniffと長さを設定する。欠損・未設定・不正画像は404、停止・起動失敗・一般障害は503とし、固定本文だけを返す。解放失敗・DB停止・危険な保存先の印は、併発する不存在より優先する。

Appの初期化完了をロックなしで待ち、リクエストキャンセルに応答する。読み取り中だけAppの読取ロックを保持してshutdownによるDB終了を防ぐ。Applicationは同じ画像操作制御・参照排他を使う。全検証と解放が終わったバイト列を返し、送信前にAppのロックも返す。送信失敗で参照を変更・自動修復しない。

Middlewareインスタンスごとに4件の枠を設け、読み取り開始からResponseWriter.Writeの終了まで保持する。枠待ちはリクエストキャンセルで終了し、App/参照ロックや操作票を持たない。これはMiddlewareの同時バッファ数の制限であり、待機リクエスト数、Wails/OS/通信層のコピーを含む全メモリ上限を保証しない。

起動時整理中は既存の画像受付が拒否するため503となる。画像UI接続では整理完了後の取得を行う。停止・終了受付後は開始済み読み取りだけ収束し、新規画像取得は進めない。

## 自動確認

- `TestCoverHTTPThroughWailsHandlers`: 開発用外部ハンドラーと内蔵アセットハンドラーで、一時DB/実生成PNGの未設定・設定・変更・欠損・破損・解除とヘッダー、読み取り時の作品情報保持を確認する。
- `TestCoverHTTPRejectsBeforeReading`: 非正規ID・overflow・余分なパス・エンコード・クエリ・メソッドをデータ読み取り前に拒否し、通常アセットへは干渉しない。
- `TestCoverHTTPDoesNotExposeErrors`: 生の絶対パス/データを含む合成エラーを公開せず、停止印を不存在より優先する。
- `TestCoverHTTPTransmissionDoesNotHoldAppAndBoundsBuffers`: 4件のWriteを待機させ、5件目のキャンセルと、送信中のApp終了が完了することを確認する。
- `TestCoverHTTPStartupWaitCanCancel`と`TestCoverHTTPReadProtectsDatabaseLifetime`: 初期化前キャンセル、終了後の拒否、読み取り解放までのApp/DB寿命を確認する。

## 限界と後続

実WebView2では未確認である。既存Spikeの実画面確認を本番接続の成功へ流用しない。画面からの再取得・仮表示・停止案内、起動時整理との実際の順序、OS/DBの実故障、長時間通信と終了、全ての並行順序は後続で確認する。ADR-0013はPendingを維持する。
