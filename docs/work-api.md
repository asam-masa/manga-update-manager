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

## 検証と制約

一時保存先でOS保存先の解決、初期化失敗、空一覧、登録、重複、ID順、日時、再オープン後の保持、終了後のAPI拒否をテストする。実利用者のDBはテストに使わない。

Issue #21では登録フォームと一覧画面を対象外とした。両画面はIssue #23で追加する。画像管理、編集・削除、ブラウザー起動、最終アクセス、更新予定、バックアップ、GitHub Actionsは後続Issueの対象である。ADR-0002とADR-0004のValidation Statusは`Pending`を維持する。
