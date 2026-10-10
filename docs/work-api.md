# 作品APIと起動・終了処理

Issue #21で、保存基盤をWails APIへ接続した。登録フォームと一覧画面は[Issue #23の画面設計](./work-screen.md)で扱う。

## 保存先と接続の寿命

`platform.DataDirectory`は`os.UserConfigDir`でOSのアプリデータ領域を解決し、`MangaUpdateManager`フォルダーを作成する。Windowsでは`%AppData%\MangaUpdateManager\manga.sqlite`へ保存する。作業フォルダーへのフォールバックは行わない。

Wailsの`OnStartup`で保存先を準備し、DB接続とマイグレーションを実行する。`OnShutdown`で接続を閉じる。API実行中は読み取りロック、起動・終了処理は書き込みロックを取り、操作中の接続を閉じない。

初期化失敗時も画面は起動する。作品APIは`database_unavailable`を返す。利用者へ内部パスやSQLiteの詳細を公開せず、保存先の空き容量・アクセス権限の確認と再起動を案内する。初期化と終了の失敗は、個人パスを含まないログにも記録する。実行中の復旧・再接続は対象外である。

## 責務

- Wails境界: 入力DTOからドメイン入力への変換、Application呼び出し、出力DTOと利用者向けエラーへの変換
- Application: 時刻取得、ドメインによる正規化・検証、保存処理の呼び出し
- manga: 作品の規則と重複URLの共通エラー
- storage: SQL、ID昇順の一覧、SQLiteのUNIQUEエラーから重複URLエラーへの変換
- platform: OS保存先と日本時間の変換

Applicationの`WorkRepository`は登録と一覧だけを定義する。時刻は関数として受け取り、固定時刻でテストできる。既存ADR-0002とADR-0004に従う局所的な実装であり、新しい依存ライブラリとADRは追加しない。

## API契約

| API | 入力 | 正常結果 |
| --- | --- | --- |
| `CreateWork` | `url`, `title`, `siteName`, `thumbnailPath`, `notes` | 採番済みの作品DTO |
| `ListWorks` | なし | ID昇順の作品DTO配列。未登録時は`[]` |
| `OpenWork` | 作品ID | 起動要求と日時保存に成功した作品DTO |

作品DTOは入力項目に加え、`id`, `createdAt`, `updatedAt`を持つ。日時は`Asia/Tokyo`のRFC 3339文字列（`+09:00`、小数秒は必要な桁数）で返す。DB保存は既存のUTC・小数秒固定9桁を維持する。IDと登録日時は利用者入力として受け取らない。サムネイルの相対パスは既存規則で検証するだけで、画像操作は行わない。

エラーはWailsの`ErrorFormatter`から`{code, message}`としてPromiseをrejectする。生成TypeScriptの戻り値型は成功時の型のみであるため、画面側はcatchでコードを確認する。

| code | 意味 |
| --- | --- |
| `database_unavailable` | 起動前、初期化失敗、終了後でDBを利用できない |
| `invalid_input` | ドメイン入力の検証失敗 |
| `duplicate_url` | 正規化後のURLが完全一致で登録済み |
| `storage_error` | DB操作の失敗 |
| `internal_error` | その他のバックエンドエラー |

重複判定は事前検索ではなく、SQLiteのUNIQUE制約を根拠とする。現在の登録SQLでUNIQUE対象はURLだけである。制約を追加する場合はエラー変換も見直す。

## Issue #25: ページ起動と最終アクセス

`WorkOpener`は利用側の`AccessRepository`、起動関数、時刻関数を受け取る。作品をIDで取得し、保存済みURLを検証し、既定ブラウザーへ渡し、成功後の時刻を保存する。同じIDは処理中だけ排他し、異なる作品を一律に止めない。終了処理は実行中APIの完了を待つ。

DTOの`lastAccessedAt`は未アクセスならJSONのnull、それ以外は日本時間のRFC 3339文字列とする。Wailsの生成型は`lastAccessedAt?: string`となるため、画面側の`WorkView`でnullを含める。生成された型は手編集しない。

起動失敗は`browser_open_failed`、起動成功後の保存失敗は`access_save_failed`、存在しないIDは`work_not_found`、処理中の同じIDは`work_open_in_progress`を返す。保存失敗は部分成功であり、ページは閉じず、画面は保存済み日時を維持する。内部診断情報は利用者へ返さない。

Windowsでは`ShellExecuteW`のエラーを返す`windows.ShellExecute`を使用する。OSスレッドを固定してCOMをSTAで初期化し、終了時に解放する。既存依存の`golang.org/x/sys`と`github.com/go-ole/go-ole`を直接依存へ変更し、新しいモジュールは導入しない。エラーを返さないWailsの`BrowserOpenURL`は今回使用しない。対象OSはWindowsである。

画面のURLはリンク風のbuttonとし、シェルへURLを渡す処理はGo側だけが担当する。Enter・Spaceで操作でき、処理中は無効になる。URLの全文はtitle属性、表示は1行省略とする。タイトルは通常の文字列のまま残す。一覧取得中に保存が完了しても古い応答が日時を戻さないよう、読取開始後の保存結果を反映する。

一次情報: [ShellExecuteW](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecutew)、[CoInitializeEx](https://learn.microsoft.com/en-us/windows/win32/api/combaseapi/nf-combaseapi-coinitializeex)。要求の成功はページ表示や読了の保証ではない。

## Issue #27: 表示設定API

Issue #27で表示設定のApplicationとAPIを追加する。作品APIとは独立した操作であり、設定エラーは作品用コードへ混在させない。

| API | 入力 | 正常結果 |
| --- | --- | --- |
| `LoadDisplaySettings` | なし | `registrationFormVisible: boolean`のDTO。未登録ならtrue |
| `SetRegistrationFormVisible` | boolean | 成功時はvoid |

設定の未登録は正常扱いとし、読み取りだけでは書き込まない。読み取り失敗・未知値・DB利用不能は`settings_read_failed`、保存失敗・DB利用不能は`settings_save_failed`の安全な文言でrejectする。画面は読み取り失敗を表示の初期値で補い、保存失敗でも操作を継続する。明示操作の保存だけを行い、作品データや診断情報を返さない。新しいライブラリは追加しない。

## 検証と制約

一時保存先でOS保存先の解決、初期化失敗、空一覧、登録、重複、ID順、日時、再オープン後の保持、終了後のAPI拒否をテストする。実利用者のDBはテストに使わない。

Issue #21では登録フォームと一覧画面を対象外とし、Issue #23で追加した。ブラウザー起動と最終アクセスはIssue #25で追加した。画像管理、編集・削除、更新予定、バックアップ、GitHub Actionsは後続Issueの対象である。ADR-0002とADR-0004のValidation Statusは`Pending`を維持する。
