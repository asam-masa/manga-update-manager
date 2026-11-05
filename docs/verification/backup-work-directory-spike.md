# バックアップDB作業フォルダーの限定Spike（#446）

## 結果と範囲

2026-11-05、Windows 10.0.26300.0、既存のgolang.org/x/sys/windowsとGoで、CreateDirectory2Wの作成時ハンドルを使う候補を合成一時領域で確認した。ローカルNTFSの親を既存のHoldBackupDirectoryで確認・保持し、DirectoryFlags=0で作成する条件が成立した。製品の採用・対応OS変更・DBコピーへの接続は行っていない。

調査上限30分に対し、公開仕様確認後の検証コード作成から結果取得までは約3分21秒（14:07:17〜14:10:38 UTC）。[再現コード](../../tools/spikes/backup-work-directory/directory_windows_test.go)はt.TempDir内の合成データだけを操作し、通常のアプリデータ・ACL・OS設定を変更しない。Goテストの一時領域の片付けは試験のためのもので、製品の削除手順ではない。

## 公開仕様

- [CreateDirectory2W](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createdirectory2w)は作成時にHANDLEを返し、アクセス権・共有モードを指定する。最小クライアントはWindows 11 24H2、サーバーはWindows Server 2025。現在の製品の最小対応OSを確定した記録ではない。
- [公開SDKの宣言](https://github.com/microsoft/win32metadata/blob/main/generation/WinSDK/RecompiledIdlHeaders/um/fileapi.h)は5引数である。Learnの例は6引数を記載しているが、試験はSyntaxとSDK宣言に合わせて5引数で呼ぶ。仕様と例の不一致を追加の引数で推測して補わない。
- [従来の作成API](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createdirectoryw)はハンドルを返さず、CreateFileで後から開く。作成から保持までの間隔があるため、同じ保証の代替として製品へ自動採用しない。
- [FILE_ID_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_id_info)でボリュームと128bit IDを比較し、[FILE_DISPOSITION_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_disposition_info)で同じ保持ハンドルへ削除を指定した。空のフォルダーだけを対象にする。

## 実測

`go test ./tools/spikes/backup-work-directory -v -count=1`を通常権限で実行した。パッケージの成功だけで全組合せを成功扱いせず、SKIPを区別する。

| 条件 | 観測結果 |
| --- | --- |
| APIの動的取得 | 手元で利用可能。存在しなければ試験を理由付きSKIPする |
| 一覧・同期アクセス、読取共有、Flags=0 | 最小条件で作成できた。属性取得に必要なアクセスを指定していないため、これだけでID取得可能とはしない |
| 一覧・属性・同期アクセス、読取/書込共有、Flags=0 | 作成とID取得が成功 |
| 上記にDELETEアクセスを追加、Flags=0、親を保持 | 既存名の新規作成を拒否、保持中の改名は失敗、合成子ファイルの読み書きと同じIDの再確認が成功 |
| 保持ハンドルへ非空フォルダーの削除指定 | ERROR_DIR_NOT_EMPTYで拒否。子ファイルは残り内容も一致。Close後に改名可能 |
| 保持ハンドルへ空フォルダーの削除指定 | 指定前後のIDが一致し、Close後に元の名前の不在を確認 |
| 属性アクセス有無ではなく、Flags=1を加えた2組合せ | 手元ではERROR_INVALID_PARAMETER。能力未確認として2サブテストをSKIP。原因をOS全体の非対応とは断定しない |

初回は汎用読取アクセスとDELETE・Flags=1、次に具体的なアクセス権とFlags=1で失敗した。その後の限定比較でFlags=0が成立した。最小条件の作成後に属性確認も試みた際はアクセス拒否になったため、最小試験の主張は作成のみとし、属性アクセスを持つ組合せで別に識別を確認した。

## 限界と後続

推奨候補は、公開APIが使える環境で既存の親確認・保持とFlags=0の新規作成を組み合わせる方式である。ただし、作成物の名前の再確認、解放失敗、途中取消、作成直後の識別失敗、想定外の内容、DBコピーとの寿命、部分的な片付けの結果型を製品契約として具体化する必要がある。識別できない物や未知の内容をパス指定で再帰削除しない。

親保持後の悪意ある属性変更・ドライブ再割当て等の全競合、実USB、電源断・強制終了、実DBの取得は未検証。Flags=1の失敗原因と、未対応OSの実測も未確認である。APIの有無を確認して安全に失敗させるか、別方式を調査するかを採用前に判断する。既存の通常機能まで最小OSを引き上げることは、このSpikeからは決定しない。

製品の設計正本は[初回設計](../backup-restore.md)と[ADR-0022](../adr/0022-manual-backup-and-replacement-restore.md)。限定成功・CI成功を製品の作成・片付け完了へ読み替えず、Pendingを維持する。
