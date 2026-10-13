# 管理記録と同一ハンドル削除の接続検証

Issue #93。先行する[状態遷移と候補取得](./managed-cover-state.md)、[実ファイル照合](./cover-file-identity.md)、[Windows削除境界](./cover-file-boundaries.md)を[テスト専用試作](../../tools/spikes/cover-file-boundaries/managed_cleanup_windows_test.go)で接続する。一時SQLiteと合成ファイルだけを使い、本番から呼び出さない。

## 問いと観測

保存確認済み・全作品から未参照の候補を読み、共有なしの読取・DELETE権限のハンドルで開く。属性・リンク数・128bit識別情報・サイズ・SHA-256を同じハンドルで照合する。全項目が一致した場合だけ、そのハンドルに`FileDispositionInfo`を渡し、Close後に合成ファイルの不存在を確認して管理記録を処理する。

2026-10-13、Windows・Go 1.26.9・CGO無効で、追加3テストと拒否条件10サブテストが成功した。先行テストを含む対象パッケージの16テストはskipなしで成功した。

- DB再接続後の保存確認済み・未参照ファイルは削除され、記録もなくなる。
- 予約、管理記録なし、2作品のうち1作品だけ参照解除、閉じたDB、同一サイズの内容変更、同一内容の別ファイル、hard link、symlink、共有なしの既存ハンドル、読取専用属性では削除しない。元の記録とファイル内容を維持する。記録不存在のケースでは不存在を維持する。
- ファイル削除後、DBのDELETEをトリガーで中止させると、ファイルはなくなり記録だけ残る。再接続後にも残り、試作の再実行はファイル不存在のエラーで止まる。これを復旧完了やファイル復元とは扱わない。

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./tools/spikes/cover-file-boundaries -v
go test ./...
go vet ./...
```

全体の検証結果と固定したレビュー版はPRへ記録する。GUI・配布exeは対象外である。

## 限界と後続

試作は単一スレッドで、固定した合成パスと管理IDだけを扱う。祖先の保護やファイル不存在の安全な証明を実装していないため、本番の削除許可に使えない。候補取得から削除までの参照変更・生成との競合排他、本番作品テーブル、未知パス、ディレクトリ拒否、Close失敗、削除成功後のDB失敗からの安全な記録整理・再試行は残る条件である。PNG生成・書込耐久性・ReFS実機・ネットワーク保存先は今回の対象外である。

本番は[画像保存契約](../cover-persistence.md)に従って段階的に実装する。Issue #42は未完了、ADR-0013はPendingを維持する。

## 一次資料

- [SetFileInformationByHandle](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-setfileinformationbyhandle)
- [FILE_DISPOSITION_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_disposition_info)
