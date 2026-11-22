# 保持中の復元準備DBと読取専用VFSの接続Spike

Issue #653、親 #651 / #378。利用者が承認した30分上限で、#647の借用ReadAtを既存SQLiteライブラリへ渡せるか確認した。技術試行は2026-11-22 17:47:01〜18:01:28 JST（14分27秒）で終了した。候補経路の限定確認であり、製品への採用・接続は行っていない。

## 対象と方法

- Windows、Go、固定済み`modernc.org/sqlite v1.61.1`の公開`vfs.New(fs.FS)`を使用する。新規依存やライブラリ内部の改変はない。
- 検証コードは`tools/spikes/restore-vfs/readonly_windows_test.go`。合成SQLiteを作成して閉じ、ZIP化→`application.PrepareRestore`で展開・読戻ししたDBを使う。
- 合成DBはsample・parent・childの小さな独自スキーマである。目録の移行名は形式を満たすfixture用であり、アプリの実スキーマ互換性・履歴一致を検証したDBではない。
- `fs.FS`は固定の論理名1件だけを受け入れ、`Open`ごとに独立した`io.SectionReader`を作る。SQLiteが必要とするRead/Seek/Statへ借用ReadAtと既知サイズを対応付ける。ホストのパスを開き直さない。
- viewのCloseは参照数を減らすだけで、準備ファイルを閉じない。SQL接続を閉じた後にVFS登録を閉じ、最後に準備物の所有者を片付ける。

一時フォルダーはハンドルから取得した正式名で合成fixtureを作る。CIの8.3短縮名への対応は試験側だけで行い、製品のパス・保持条件を変更しない。

## 実行結果

`go test ./tools/spikes/restore-vfs -count=1 -v`は3試験・6サブテストが成功し、skipはなかった。

| 確認 | 観測した結果 |
| --- | --- |
| 基本読取 | 保持した準備DBから既知の値をSELECTできた |
| 整合性検査 | `integrity_check`が`ok`、正常な外部キー関係では`foreign_key_check`が0行だった |
| 既知の外部キー違反 | 違反する合成DBでは`foreign_key_check`が行を返した |
| 書込み | DSNのmode=roとmode=rwの両方で、INSERT/UPDATE/DELETE/CREATEとjournal_mode=OFF後のINSERTがreadonlyエラー（8）で拒否された |
| クエリー取消 | 100msの期限付き再帰集計は`context deadline exceeded`で終了し、その後の通常SELECTは成功した |
| VFSの解放順 | DB使用中のVFS Closeは`ErrInUse`、DB Close後はview数0になりVFS Closeが成功した |
| 元の保持 | SQL/VFS解放後も準備DBを同じReaderから読め、全本文のサイズ・SHA-256が変わらなかった |
| 論理名以外 | `../fixture.sqlite`のOpenは拒否した。任意パスを扱うFSではない |

書込拒否は試したSQLと固定版での観測であり、任意SQLや任意DBに対する完全なサンドボックスの証明ではない。取消もCPUを使う合成クエリーでの結果であり、ブロック中のOS I/Oを強制中断できる保証ではない。

## 読取原因の見落としと必要条件

下位ReadAtが有効バイトと非EOFエラーを同時に返す故障を入れた。

| 模型 | SQL側の結果 | 読取側に保持した原因 |
| --- | --- | --- |
| 原因を記録するだけで読み続ける診断用条件 | SELECTが成功（エラーnil） | 合成の読取エラーが残った |
| 最初の原因を保持し、以後の読取を拒否 | disk I/O error（266） | 同じ合成エラーが残った |
| 読取側のcontextが取消済み | disk I/O error（10） | `context.Canceled`が残った |

SQLのエラーだけを検査しても、元のGoの原因は失われたり、SQLが成功扱いになったりする。候補を採用する場合は、最初の非EOF原因を保持して以後のI/Oを拒否し、SQL結果・保持原因・contextを合わせて各処理と終了時を判定する契約が必要である。診断用の「記録だけ」の条件を製品へ転用しない。

固定版のVFS実装ではRead/Seekを使い、Go側の失敗をSQLiteの戻り値へ変換する。view CloseのエラーをSQLiteへ伝える経路もない。今回のview Closeはネイティブ資源を解放しない操作とし、SQL接続・VFS登録・view数・準備物の本来の解放を別々に確認した。製品用の解放未確認や失敗時の保全契約は、まだ具体化していない。

参照した一次根拠は固定依存の`modernc.org/sqlite@v1.61.1/vfs/patches.go`（`New`、`vfsOpen`、`vfsRead`、`vfsClose`、`FS.Close`）と、本リポジトリの準備物・ReadAt契約である。固定版以外の動作を保証しない。

## 結論と後続

後続の判断：2026-11-22、#655で[必要条件付きの採用と解放契約](../restore-sql-session.md)を承認・記録した。以下の採用未決定はSpike時点の状態であり、製品契約の正本は後続文書とする。製品実装・検証完了を意味する更新ではない。

保持解放・パス再取得・DB一式の追加コピーなしに、借用Readerを公開の読取専用VFSへ接続する候補は成立した。VFS採用はまだ決定していない。#651で読取原因の保持、SQL/VFS/viewの寿命と解放未確認、contextと所有者の関係を含む製品契約と採否を検討する。

未知/未来のDB構造、対応旧版の許可リストと移行、アプリの全データ規則、画像内容・寸法とDB参照、物理識別再確立は未検証である。巨大DB・悪意ある全入力・同時複数SQL接続・実OS故障・電源断・App/UI・通常再開・退避/切替/復旧にも広げていない。ADR-0022はPendingを維持する。
