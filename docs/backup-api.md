# バックアップの公開API

#579で取消・除外詳細、#581で状況、#583で結果の案内と再実行可否、#586で保存先と残置候補を公開する。保存先選択/開始APIは#573の後続であり、製品画面は未接続である。保存と安全条件の正本は[バックアップ設計](./backup-restore.md)、判断は[ADR-0022](./adr/0022-manual-backup-and-replacement-restore.md)を参照する。

## 識別子と観測

`executionId`は0以外のuint64を正規10進文字列で渡す。JavaScriptの数値へ変換しない。符号・空白・先頭0・小数・指数表記・範囲外を受け付けない。実行IDはアプリ起動中に再利用せず、応答のIDと表示対象のIDを照合する。次回開始や終了で保持されなくなった結果の詳細は再取得できない。

関連作品の`workId`も文字列で返す。タイトルはバックアップ時に確認・保持した名前で、現在の作品一覧を再検索した値ではない。診断エラーと除外画像の内部相対パスは公開しない。作業物の場所は専用の`GetBackupLocations`だけで返し、取消・状況・結果案内・除外詳細のAPIへは含めない。

## メソッド

| メソッド | 入力 | 応答 |
| --- | --- | --- |
| `GetBackupStatus` | なし | `BackupStatusDTO`: 同一実行の状態・経過と保持中の観測概要 |
| `GetBackupResult` | `executionID: string` | `BackupResultDTO`: 保持中の結果案内と再実行可否 |
| `GetBackupLocations` | `executionID: string` | `BackupLocationsDTO`: 保存先・公開確認先・残置候補 |
| `CancelBackup` | `executionID: string` | `BackupCancelDTO`: `executionId`と`decision` |
| `GetBackupOmittedImages` | `executionID: string, page: int` | `BackupOmittedImagesDTO`: ページ情報と除外画像 |
| `GetBackupOmittedImageWorks` | `executionID: string, imageID: string, page: int` | `BackupOmittedWorksDTO`: ページ情報・`imageId`と対象作品 |

取消・取得は通常操作ticketやDB読込を使わず、保存による受付停止中や資源解放未確認による停止後も利用できる。取消だけが状態を変え、取得では開始・取消・結果消去を行わない。取得失敗や応答欠落を処理完了と扱わない。

### 状況と結果の観測概要（#581）

`executionId, state, stage, elapsedSeconds, canCancel, result`を返す。未開始・保持終了後はIDが空文字、`state: idle`、`stage: unobserved`、`result: null`とする。idleは保存開始可能という保証ではない。通常は`running, cancelling, publishing, finished`の状態を返し、未対応状態は`unknown`として取消を有効にしない。

段階は最後に観測した作業であり、`publishing`という段階表示だけでは取消不可にならない。`canCancel`は実行制御の観測を使う。段階未観測は`unobserved`で、完了やフリーズとは判断しない。経過秒は開始受付からの時間を非負の整数秒へ切り捨て、完了後は保持時の値を固定する。I/Oの残り時間や正確な進捗率ではない。

保持中の`result`には`publication, hasWarnings, omittedImageCount, workNamesState, workCount`を返す。公開状態は`not_attempted, collision, confirmed, unknown`で、未対応値も`unknown`とする。公開確認と後処理・資源解放の成否は別であり、`confirmed`だけで再開・再実行を許可しない。警告フラグは処理失敗、除外画像、詳細利用不可など注意すべき観測も含み、原因や対処の完全な分類ではない。

件数未観測は`null`、観測済み空は0である。作品名の可用性は`unobserved, available, unavailable`とし、利用不可も0件へ変換しない。Wails生成型ではnullable値がoptionalとして表れるため、画面接続ではnull/undefinedの両方を未取得として扱う。この状況APIでは保存先や残置候補のパス、診断エラー、目録・全詳細を返さない。結果の案内と再実行可否は下記`GetBackupResult`で返し、開始APIとの接続は後続とする。

### 結果の案内と再実行可否（#583）

`executionId, outcome, message, cancellationObserved, retryAllowed, notices`を返す。旧ID/結果未保持は既存の型付きエラーとし、観測に状態変更やDB I/Oを伴わない。保存先・残置候補のパスはまだ返さない。

| outcome | 意味 |
| --- | --- |
| `saved` | 公開を確認し、保持した結果に警告がない |
| `saved_with_warnings` | 公開は確認したが、除外・詳細不足・後処理/最終安全性などに注意がある |
| `not_saved` | 公開を開始していない。取消や処理失敗と警告を確認する |
| `collision` | 同名ファイルを上書きせず中止した |
| `unknown` | 公開の成否不明または未対応状態。候補を保全し、再保存しない |

`cancellationObserved`は取消を含む原因を検出した観測であり、利用者の取消受付や片付け成功の証明ではない。取消と別の失敗が混ざった場合も`not_saved`と注意事項を残し、単純な取消成功へ置き換えない。`notices`は固定した安全なcode/messageの配列で、処理未完了、公開後の警告、取消検出、開始前の警告、画像除外、作品名利用不可、片付け警告、再実行停止を区別する。任意の内部エラー本文や警告文字列は公開しない。

処理の所有者は最終再開判定時に、資源解放と既存の停止状態を確認できたかを`RetrySafetyConfirmed`として保持する。worker開始前の取消では新しいworker資源を持たないことを踏まえ、開始前整理の未確認/警告と既存停止を除外して確認する。エラーがnilというだけでは確認済みにせず、未観測はfalseを既定とする。

`retryAllowed`は公開状態が対応済みで、上の確認があり、最終判定エラーがない場合だけtrueとする。成否不明・未対応状態では全解放確認済みでもfalse。これは「前回結果が新しいバックアップを妨げない」という記録時の判断であり、現在の開始可能性や通常操作の再開権限ではない。開始時にはApp寿命・既存受付/停止・保存先の安全条件を必ず再確認する。公開確認後の警告や残置だけで、全解放確認済みの再実行まで一律に止めない。再起動だけで成否不明が解消するとは案内しない。

### 保存先と残置候補（#586）

`GetBackupLocations`は結果保持後に、同一世代の`executionId, destination, publication, confirmedPath, candidates`を返す。`destination`は開始時の選択先で、保存成功の証明ではない。`publication`は状況APIと同じ4種類とし、`confirmedPath`は公開確認済みの完成名だけを返し、それ以外は空文字にする。

`candidates`は`kind, path`の配列で、ZIP一時物（`temporary_zip`）、成否不明の公開先（`publication_target`）、DBコピー（`database_copy`）、作業フォルダー（`work_directory`）を区別する。不在確認済みの物と公開確認後の元一時名は含めない。削除指定だけでは不在と判断しない。作成失敗時は型付きの場所観測を使い、作成成功未確認の場所も候補として残す。同じ種類・同じ文字列の重複はまとめ、未観測の場所を推測しない。空配列は記録された候補がない意味であり、残置物の不存在を保証しない。

場所は処理時点の記録であり、現在の存在・同一性・削除権限を保証しない。取得ではファイル/DBの読込、開く操作、削除、再実行、受付状態の変更を行わない。診断本文・元DBパス・画像内部パスを連結せず、この専用APIだけで目的に必要な場所を返す。旧ID・未完了は結果APIと同じエラーにする。製品画面と保存先選択/開始・実Wails結合は引き続き#573の後続である。

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
