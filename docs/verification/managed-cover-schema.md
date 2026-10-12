# 生成画像管理テーブルの制約検証

Issue #81のSpike。一時SQLiteの候補テーブルを[検証コード](../../tools/spikes/managed-cover-schema/schema_test.go)だけで定義する。本番マイグレーションへは追加しない。利用者DB・ファイル・画像を使わない。

## 候補と確認項目

`managed_covers`のIDは32文字の小文字16進数、相対パスは`covers/<ID>.png`の生成列とする。既存作品のパスをこのテーブルへ自動登録しない。状態は`reserved`と`ready`で区別する。

`reserved`の確認情報はNULLのみ。`ready`はvolume serial（8文字）・file ID（16文字）の小文字16進表記、正のサイズ、32 byteのSHA-256 BLOBを必須とする。これは従来Windows APIの識別情報を格納する候補であり、異なるOS・ファイルシステムへそのまま適用しない。

合成レコードで、正常な状態遷移と生成パス、重複ID拒否、不正・NULL ID、未知状態、確認情報不足、予約へ確認情報が混在する不正状態を確認する。各確認項目の不足・不正値と、失敗した更新・挿入で部分的な値を残さないことも確認する。

SQLiteのCHECKはNULL評価だけでは拒否にならないため、保存確認済みの必須列には`IS NOT NULL`も指定する。STRICTの型制約と状態の整合条件を分けて検証する。

## 実行

2026-10-12、Go 1.26.9・`CGO_ENABLED=0`で検証した。初回は4テストが成功したが、通常・独立レビューで埋め込みNULの見落としを確認した。NUL入りIDが受理されることを失敗テストで再現し、ID・volume serial・file IDに`instr(column,char(0))=0`を追加した。修正後の対象は5テスト（不正レコード5件・照合情報12件のサブテストを含む）。結果はPRへ記録する。実ファイル操作と本番マイグレーションは実行していない。

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./tools/spikes/managed-cover-schema -v
go vet ./tools/spikes/managed-cover-schema
```

## 保証しないこと

列・制約が成立しても、そのファイルをアプリが生成したという事実、OS識別の永続性、ハッシュの実ファイル一致、競合排他、ファイル・DBの完全な原子性を証明しない。作成予定の記録は削除許可に使わない。本番採用は[画像保存契約](../cover-persistence.md)に従い、OS境界と合わせて検証する。Issue #42は未完了、ADR-0013はPendingを維持する。

## 一次資料

- [SQLite CREATE TABLEとCHECK制約](https://www.sqlite.org/lang_createtable.html)
- [SQLite STRICT tables](https://www.sqlite.org/stricttables.html)
- [SQLiteの埋め込みNULとlength](https://www.sqlite.org/nulinstr.html)
