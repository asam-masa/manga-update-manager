# 画像管理領域の祖先保持を検証する

Issue #103のWindows専用Spike。[専用テスト](../../tools/spikes/cover-file-boundaries/ancestors_windows_test.go)は`t.TempDir()`に作った固定の`root/managed/covers`だけを扱い、本番から呼び出さない。利用者画像・DB・ACL・システム設定は変更しない。

## 問いと観測

2026-10-14、Windows・Go 1.26.9・CGO無効で追加3テストがskipなしで成功した。

- coversだけを共有なしで保持した場合も、この環境では親managedの移動がAccess deniedで拒否された。環境依存の観測であり、子の保持だけで全祖先が保護されるとは結論しない。
- root、managed、coversを順に開き、同じハンドルの種類・ディレクトリ属性・reparse属性を確認して保持した。各階層の移動は拒否された。子の確認が終わるまで親ハンドルを解放しない。
- 全階層の保持中に合成ファイルをCREATE_NEWで作成し、書き込み・同期・Closeを完了した。保持を解放すると各階層を移動でき、合成内容も維持された。
- managedをディレクトリsymlinkへ置き換えた場合、属性検証のエラーを確認した。リンク先の合成内容と件数は不変で、失敗後のroot移動も成功した。部分取得のハンドルを解放したことを併せて確認する。

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./tools/spikes/cover-file-boundaries -v
go test ./...
go vet ./...
```

## 判断材料と限界

固定された起点から各ディレクトリを順に確認・保持する方式は、今回の合成階層で次の候補になった。ただし本番採用のDecisionではない。絶対パスで最初のrootを開く時点と、rootより上の祖先は保護していない。アプリ保存先の信頼起点、起点までのreparse point、取得中の並行差し替えや属性変更、junction・全reparse tag、ネットワーク・ReFSは未検証である。

移動拒否は共有違反またはアクセス拒否として確認し、解放後の移動成功も要求する。子だけ保持する観測テストはどちらの結果も記録できるため、そのテストのPASSだけを安全性の証明にしない。symlink拒否では任意のOSエラーを成功とせず、属性検証の拒否を要求する。

共有なしのディレクトリハンドルは他の操作を制限し得る。実アプリの保存・削除・配信での同時利用や保持寿命は別途決める。生成画像の所有、全作品の未参照、同一ハンドル削除、不存在の安全な証明、DBとファイルの部分成功もこのSpikeでは証明しない。Close失敗の注入や復旧は未検証。画像操作全体とADR-0013はPendingのまま維持する。

## 一次資料

- [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)：共有モード、ディレクトリの開き方、reparse point用のフラグ
