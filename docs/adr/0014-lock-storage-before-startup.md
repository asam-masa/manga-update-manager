# ADR-0014: DB接続より先に保存先の排他を確保する

- Status: Accepted
- Validation Status: Verified
- Date: 2026-10-11
- Related: [#58](https://github.com/asam-masa/manga-update-manager/issues/58)、[#42](https://github.com/asam-masa/manga-update-manager/issues/42)

## Context

ADR-0013で同じ保存先の二重起動を禁止した。画像整理より先に排他が必要であり、DB接続も同じ境界の後に置く。ユーザーは排他を確認できなければ、DB・画像へアクセスせず案内して起動を中止する方針と、画像操作から分離した先行実装を承認した。

採用済みWails v2.14.0のWindows SingleInstanceLockは、他の起動を発見しても通知先ウィンドウがない場合などに起動を続ける。安全な保存先排他の唯一の根拠にはしない。今回の対象はWindowsの起動条件であり、画像整理・画像API・ウィンドウの前面化は対象外である。

## Decision Drivers

- 排他を確認できない状態で保存データへ触れない
- 保存先が同じならパスの大文字・小文字や別名で迂回できない
- 正常終了・異常終了のどちらでも再起動できる
- Windowsセッションごとの名前付きmutexの識別や、PID・残存ファイルの寿命判定を増やさない

## Considered Options

### WailsのSingleInstanceLockだけを使う

追加処理は少ないが、失敗時の継続と保存先単位の識別を別に保証する必要がある。

### 保存先から名前付きmutexのキーを作る

OSが寿命を管理できるが、Windowsセッションの範囲、パスの別名、同じ実体の識別、名前衝突と失敗時の扱いが増える。

### 固定ファイルを共有なしで開き続ける

実際のファイルに対するWindowsの共有制御で識別でき、プロセス終了時に解放される。固定ファイルを保存先へ一つ作る負担があり、共有制御が使えない環境では起動を拒否する。

## Decision

WindowsのCreateFileWで、保存先の`.instance.lock`を読み書きアクセス・共有なし・OPEN_ALWAYSで開き、Wails実行とDB終了処理が終わるまでハンドルを保持する。既存のgolang.org/x/sys/windowsを使用し、新規依存は追加しない。

OPEN_ALWAYSは既存内容を切り詰めない。排他はファイルの存在ではなく開いたハンドルで判断し、終了時にもロックファイルを削除しない。共有なしにより、保持中の読み書きアクセスと削除・名前変更を防ぐ。ハンドルは子プロセスへ継承しない。

ロックファイル自体のreparse pointをたどらず、取得したハンドルの属性・種類・リンク数を確認する。ディレクトリ、reparse point、複数hard linkなどは拒否する。保存先ディレクトリの別名は許容し、その配下の同じ実体のロックを使う。保存先やその祖先を外部から実行中に付け替える攻撃の完全な防御は、この起動排他の保証に含めない。

保存先を一度解決して作成し、排他確保後にその同じ保存先をDB初期化へ渡す。排他前は保存先の準備とロック取得だけを許し、DB接続・画像操作は行わない。共有違反は起動中として案内し、その他の失敗は安全に起動できないとして案内し、どちらも終了する。内部パスやOS診断は表示しない。画面作成前でも案内できるWindows MessageBoxを使い、表示失敗時は安全な文言だけを標準エラーへ出す。

正常終了・Wails起動失敗・run関数のpanic時は、DB終了処理を先に行い、その後でハンドルを閉じる。WailsのOnStartupは別goroutineで動くため、Appの同じmutexで終了済み状態を保持し、終了後の遅延した起動コールバックは初期化せず返る。既に開始した初期化とAPI操作は終了処理が待つ。再起動は新しいAppを使う。プロセスの強制終了時はOSがハンドルを解放する。既存ウィンドウの前面化や、先行プロセスへの引数転送は行わない。

## Consequences

同じ保存先は一つのアプリだけが使える。別保存先の検証用アプリは同時に使える。共有違反の原因が別アプリのロックファイル操作だった場合も、安全側に起動を止める。

保存先の準備・排他に失敗した場合は画面を起動しない。排他取得後のSQLite初期化失敗は、従来どおり画面を起動してdatabase_unavailableで案内する。排他はその画面を閉じるまで維持する。

古い版はこの排他に参加しないため、新旧版の同時実行は防げない。更新時は既に起動中の旧版を閉じる。ネットワーク保存先・複数Windowsセッションでの実測や、実行中の保存先の外部変更は未検証である。画像保存・削除の安全性は後続Issueで個別に検証する。

## Validation

Go 1.25.0・CGO無効の実Windowsで、同じ保存先の別プロセス拒否、別保存先の同時利用、正常終了・強制終了後の再取得、8件の同時取得、大文字表記・正規化した別名・ディレクトリsymlink別名、保持中の削除・名前変更拒否、既存内容保持、hard link・symlink拒否を確認した。初回sandboxではリンク作成が拒否され、承認済みの通常権限で再実行した際はskipなしで成功した。

起動境界のテストで、保存先・排他失敗時にWailsを呼ばずDBを作らないこと、保存先の解決が一回だけであること、DB終了後に排他を解放すること、起動エラーとpanic時の解放を確認した。独立レビューで起動コールバックが終了後まで遅延する競合を指摘されたため、終了済みガードと再現テストを追加した。2026-10-12、開始済み初期化の終了待ちと、ロック解放後の遅延コールバックがDBを作らないことを確認し、全Goテスト・vetも再確認した。

2026-10-11、専用APPDATA・TEMPの実exeを表示してUI Automationで確認した。通常画面、二重起動の案内と終了、別保存先の同時起動、正常終了・強制終了後の再起動、取得不能時の安全な案内とDB未作成が成功した。全Goテスト・vet、既存画面44テスト、TypeScript/Viteビルド、Windows exeビルドも成功した。初回の非表示起動ではウィンドウの観測ができず、利用者が表示を承認した後に再実行した。人による目視・キーボード確認や、上記の未検証環境までVerifiedの範囲へ含めない。実測結果は[検証記録](../verification/storage-instance-lock.md)へ記録する。

2026-10-12、終了済みガード追加後のexeを再ビルドし、同じ6観測が成功した。

## References

- [ADR-0013](./0013-define-cover-operation-boundaries.md)
- [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)
- [GetFileInformationByHandle](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfileinformationbyhandle)
- [MessageBoxW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-messageboxw)
- [Wails v2.14.0 Windows SingleInstanceLock](https://github.com/wailsapp/wails/blob/v2.14.0/internal/frontend/desktop/windows/single_instance.go)
