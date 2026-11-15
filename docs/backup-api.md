# バックアップの公開API

#579で取消・除外詳細の3メソッド、#581で状況と結果の観測概要を公開する。保存先/残置場所・失敗時の案内、再実行可否の分類、保存先選択/開始APIは#573の後続であり、製品画面は未接続である。保存と安全条件の正本は[バックアップ設計](./backup-restore.md)、判断は[ADR-0022](./adr/0022-manual-backup-and-replacement-restore.md)を参照する。

## 識別子と観測

`executionId`は0以外のuint64を正規10進文字列で渡す。JavaScriptの数値へ変換しない。符号・空白・先頭0・小数・指数表記・範囲外を受け付けない。実行IDはアプリ起動中に再利用せず、応答のIDと表示対象のIDを照合する。次回開始や終了で保持されなくなった結果の詳細は再取得できない。

関連作品の`workId`も文字列で返す。タイトルはバックアップ時に確認・保持した名前で、現在の作品一覧を再検索した値ではない。診断エラー、DB/一時ファイルのパス、除外画像の内部相対パスはこのAPIへ含めない。

## メソッド

| メソッド | 入力 | 応答 |
| --- | --- | --- |
| `GetBackupStatus` | なし | `BackupStatusDTO`: 同一実行の状態・経過と保持中の観測概要 |
| `CancelBackup` | `executionID: string` | `BackupCancelDTO`: `executionId`と`decision` |
| `GetBackupOmittedImages` | `executionID: string, page: int` | `BackupOmittedImagesDTO`: ページ情報と除外画像 |
| `GetBackupOmittedImageWorks` | `executionID: string, imageID: string, page: int` | `BackupOmittedWorksDTO`: ページ情報・`imageId`と対象作品 |

取消・取得は通常操作ticketやDB読込を使わず、保存による受付停止中や資源解放未確認による停止後も利用できる。取消だけが状態を変え、取得では開始・取消・結果消去を行わない。取得失敗や応答欠落を処理完了と扱わない。

### 状況と結果の観測概要（#581）

`executionId, state, stage, elapsedSeconds, canCancel, result`を返す。未開始・保持終了後はIDが空文字、`state: idle`、`stage: unobserved`、`result: null`とする。idleは保存開始可能という保証ではない。通常は`running, cancelling, publishing, finished`の状態を返し、未対応状態は`unknown`として取消を有効にしない。

段階は最後に観測した作業であり、`publishing`という段階表示だけでは取消不可にならない。`canCancel`は実行制御の観測を使う。段階未観測は`unobserved`で、完了やフリーズとは判断しない。経過秒は開始受付からの時間を非負の整数秒へ切り捨て、完了後は保持時の値を固定する。I/Oの残り時間や正確な進捗率ではない。

保持中の`result`には`publication, hasWarnings, omittedImageCount, workNamesState, workCount`を返す。公開状態は`not_attempted, collision, confirmed, unknown`で、未対応値も`unknown`とする。公開確認と後処理・資源解放の成否は別であり、`confirmed`だけで再開・再実行を許可しない。警告フラグは処理失敗、除外画像、詳細利用不可など注意すべき観測も含み、原因や対処の完全な分類ではない。

件数未観測は`null`、観測済み空は0である。作品名の可用性は`unobserved, available, unavailable`とし、利用不可も0件へ変換しない。Wails生成型ではnullable値がoptionalとして表れるため、画面接続ではnull/undefinedの両方を未取得として扱う。ここでは保存先や残置候補のパス、診断エラー、目録・全詳細を返さない。失敗時の案内と再実行可否、開始APIとの接続は後続とする。

### 取消

`decision`は`accepted`（受付）、`already_accepted`（受付済み）、`publication_started`（公開開始済みで取消不可）、`finished`（完了済み）を返す。受付は処理/片付け完了ではない。旧IDは`backup_result_expired`で拒否し、次の実行を取り消さない。

### 除外詳細

`executionId, page, pageSize, pages, total, items`を返す。ページは1始まり、`pageSize`は50固定で、観測済みの空一覧は1ページ・`items: []`とする。ページ境界を越えた要求は拒否する。別実行の応答を現在の結果へ混ぜない。

除外画像は`imageId`と`reason`を返す。理由は`missing`（欠損）、`corrupt`（変更または破損）、未対応理由の`unavailable`に限定し、内部文字列をそのまま出さない。作品名取得はその除外画像のIDを指定し、`workId, title`の配列を返す。画像件数と作品件数は異なり、共有画像の作品名もページを分けて確認できる。

## 型付きエラー

既存の`APIError`とWailsのエラー形式を使い、固定した安全な案内を返す。内部エラーの本文を連結しない。

| code | 意味・次の行動 |
| --- | --- |
| `invalid_input` | ID、画像ID、ページ指定を確認する |
| `backup_result_expired` | 対象実行は保持されていない。現在の状況を取得し直す |
| `backup_result_pending` | 処理中で結果未保持。完了後に確認する |
| `backup_details_unobserved` | 除外情報や作品名を確認できていない。0件と扱わない |
| `backup_work_names_unavailable` | 作品名詳細を利用できない。結果の警告を確認する |
| `backup_image_not_found` | 除外画像一覧を取得し直す |
| `backup_details_failed` | 未対応の取得失敗。現在の状況を取得し直す |

## 検証範囲

合成実行でIDの精度境界/不正表記、取消状態と旧ID、停止中の利用、ページ/可用性、JSONの空配列・非公開情報、返却値の所有分離を検証する。#581では合成時計で経過・完了後固定・次回開始/保持終了と件数の可用性を確認する。生成bindingとWindowsビルドを確認するが、実Wailsからの呼出し・保存ダイアログ・製品画面との結合は#573へ残す。保存先選択/開始と案内の接続が未完了のため、画面再読込から操作全体を復元できるとは扱わず、ADR-0022はPendingを維持する。
