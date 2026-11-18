import {StrictMode} from 'react';
import {cleanup, fireEvent, render, screen} from '@testing-library/react';
import {beforeEach, afterEach, expect, it, vi} from 'vitest';
import {BackupDialog} from './BackupDialog';
import type {BackupDialogProps} from './BackupDialog';
import {readBackupResult, readBackupStatus} from './backup-response';

beforeEach(() => {
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {
    cleanup();
    Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal');
    Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');
});
const idle = () => readBackupStatus({executionId: '', state: 'idle', stage: 'unobserved', elapsedSeconds: 0, canCancel: false});
const running = (state = 'running', canCancel = true, stage = 'database') => readBackupStatus({executionId: '1', state, stage, elapsedSeconds: 12, canCancel});
const finished = (count: number | null = 2) => readBackupStatus({executionId: '1', state: 'finished', stage: 'cleanup', elapsedSeconds: 16, canCancel: false,
    result: {publication: 'confirmed', hasWarnings: true, omittedImageCount: count, workNamesState: 'available', workCount: 3}});
const saved = (outcome = 'saved_with_warnings', retryAllowed = true, id = '1') => readBackupResult({executionId: id, outcome, retryAllowed,
    cancellationObserved: false, message: '保存の案内', notices: [{code: 'images_omitted', message: '画像を除外しました。'}]}, id);
function props(patch: Partial<BackupDialogProps> = {}): BackupDialogProps {
    return {status: idle(), result: null, fresh: true, selecting: false, cancelling: false, error: '',
        onStart: vi.fn(), onCancel: vi.fn(), onReload: vi.fn(), onClose: vi.fn(), ...patch};
}

it('開始前の注意を表示し、マウントだけではコマンドを呼ばない', () => {
    const p = props(); render(<StrictMode><BackupDialog {...p} /></StrictMode>);
    expect(screen.getByRole('dialog', {name: 'バックアップ'})).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByRole('heading', {name: 'バックアップ'}));
    expect(screen.getByText(/暗号化されません/)).toBeTruthy();
    expect(screen.getByText(/既存ファイルは上書きしません/)).toBeTruthy();
    expect(p.onStart).not.toHaveBeenCalled(); expect(p.onCancel).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', {name: '保存先を選んで開始'})); expect(p.onStart).toHaveBeenCalledTimes(1);
});

it('選択中は前回結果を保持し、終了と二重開始を防ぐ', () => {
    const p = props({status: finished(), result: saved(), selecting: true}); render(<BackupDialog {...p} />);
    expect(screen.getByText('保存の案内')).toBeTruthy();
    expect(screen.queryByRole('button', {name: /保存先を選/})).toBeNull();
    fireEvent.click(screen.getByRole('button', {name: '閉じる'}));
    fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true}));
    expect(p.onClose).not.toHaveBeenCalled();
});

it('段階と経過を表示し、段階名から取消可否を推測しない', () => {
    const p = props({status: running('running', true, 'publishing')}); render(<BackupDialog {...p} />);
    expect(screen.getByText('処理段階：保存確定')).toBeTruthy(); expect(screen.getByText('経過時間：12秒')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', {name: 'バックアップをキャンセル'})); expect(p.onCancel).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('button', {name: /保存先を選/})).toBeNull();
    fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true})); expect(p.onClose).not.toHaveBeenCalled();
});

it.each(['cancelling', 'publishing', 'unknown'])('状態 %s は取消・再開始・終了を有効にしない', (state) => {
    const p = props({status: running(state, false)}); render(<BackupDialog {...p} />);
    expect(screen.queryByRole('button', {name: 'バックアップをキャンセル'})).toBeNull();
    expect(screen.queryByRole('button', {name: /保存先を選/})).toBeNull();
    fireEvent.click(screen.getByRole('button', {name: '閉じる'})); expect(p.onClose).not.toHaveBeenCalled();
    if (state === 'cancelling') expect(screen.getByText(/片付け・解放が終わるまで/)).toBeTruthy();
});

it('取消要求中も片付けを待ち、完了表示へ進めない', () => {
    render(<BackupDialog {...props({status: running(), cancelling: true})} />);
    expect(screen.getByText(/片付け・解放が終わるまで/)).toBeTruthy();
    expect(screen.queryByRole('heading', {name: '保存を確認しました'})).toBeNull();
    expect(screen.queryByRole('button', {name: 'バックアップをキャンセル'})).toBeNull();
});

it('結果と画像除外を分け、閉じるまで残す', () => {
    const p = props({status: finished(), result: saved()}); render(<BackupDialog {...p} />);
    expect(screen.getByRole('heading', {name: '保存を確認しました'})).toBeTruthy();
    expect(screen.getByText('除外した画像：2件')).toBeTruthy(); expect(screen.getByText('画像を除外しました。')).toBeTruthy();
    expect(p.onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', {name: '保存先を選び直す'})); expect(p.onStart).toHaveBeenCalledTimes(1);
    fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true})); expect(p.onClose).toHaveBeenCalledTimes(1);
});

it.each([null, 0])('未取得と0件を区別する: %s', (count) => {
    render(<BackupDialog {...props({status: finished(count), result: saved()})} />);
    expect(screen.getByText(count === null ? '除外した画像：未確認' : '除外した画像：0件')).toBeTruthy();
});

it.each(['unknown', 'saved_with_warnings', 'not_saved', 'collision'])('再実行不可の結果 %s から再開始させない', (outcome) => {
    render(<BackupDialog {...props({status: finished(), result: saved(outcome, false)})} />);
    expect(screen.queryByRole('button', {name: /保存先を選/})).toBeNull();
    expect(screen.getByText(/再起動だけで保存の成否は確定しません/)).toBeTruthy();
});

it('取得未確認では結果を残して開始を止め、再読込を通知する', () => {
    const p = props({status: finished(), result: saved(), fresh: false, error: '安全な固定案内'}); render(<BackupDialog {...p} />);
    expect(screen.getByText('保存の案内')).toBeTruthy(); expect(screen.getByRole('alert').textContent).toBe('安全な固定案内');
    expect(screen.queryByRole('button', {name: /保存先を選/})).toBeNull();
    fireEvent.click(screen.getByRole('button', {name: '再読み込み'})); expect(p.onReload).toHaveBeenCalledTimes(1);
});

it('別実行の結果を表示せず、結果未取得では再開始しない', () => {
    render(<BackupDialog {...props({status: finished(), result: saved('saved', true, '2')})} />);
    expect(screen.queryByText('保存の案内')).toBeNull(); expect(screen.queryByRole('button', {name: /保存先を選/})).toBeNull();
});
