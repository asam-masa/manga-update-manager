import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {GetBackupStatus, GetBackupResult, SelectAndStartBackup} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', async () => {
    const {withLibrarySnapshot} = await import('./test-library-api');
    return withLibrarySnapshot({ListWorks: async () => [], LoadDisplaySettings: async () => ({registrationFormVisible: false}),
        SetRegistrationFormVisible: async () => {}, LoadWorkListMode: async () => 'cards', SetWorkListMode: async () => {},
        LoadWorkSort: async () => 'registration_asc', SetWorkSort: async () => {}, LoadShowFutureSchedules: async () => true,
        SetShowFutureSchedules: async () => {}, LoadCoverWarnings: async () => [], LoadCoverProgress: async () => [],
        GetBackupStatus: vi.fn(), GetBackupResult: vi.fn(), SelectAndStartBackup: vi.fn(), CancelBackup: vi.fn(),
        LoadTagUsage: vi.fn(), CreateWork: vi.fn(), OpenWork: vi.fn()});
});
const idle = {executionId: '', state: 'idle', stage: 'unobserved', elapsedSeconds: 0, canCancel: false};
const finish = {...idle, executionId: '1', state: 'finished', stage: 'cleanup', result: {
    publication: 'confirmed', hasWarnings: true, omittedImageCount: 1, workNamesState: 'available', workCount: 1}};
beforeEach(() => {
    vi.mocked(GetBackupStatus).mockReset().mockResolvedValue(main.BackupStatusDTO.createFrom(idle));
    vi.mocked(GetBackupResult).mockReset().mockResolvedValue(main.BackupResultDTO.createFrom({executionId: '1', outcome: 'saved_with_warnings',
        message: 'バックアップは保存されています。', cancellationObserved: false, retryAllowed: true,
        notices: [{code: 'images_omitted', message: '画像を除外しました。'}]}));
    vi.mocked(SelectAndStartBackup).mockReset().mockImplementation(async () => {
        vi.mocked(GetBackupStatus).mockResolvedValue(main.BackupStatusDTO.createFrom(finish));
        return main.BackupStartDTO.createFrom({cancelled: false, executionId: '1'});
    });
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});

it('入口から保存し、閉じた後も警告と保持結果を確認できる', async () => {
    render(<App />); const origin = screen.getByRole('button', {name: 'バックアップ'});
    await userEvent.click(origin); const dialog = screen.getByRole('dialog');
    await userEvent.click(await within(dialog).findByRole('button', {name: '保存先を選んで開始'}));
    await within(dialog).findByText('バックアップは保存されています。');
    fireEvent(dialog, new Event('cancel', {cancelable: true}));
    expect(screen.getByRole('alert', {name: 'バックアップのお知らせ'}).textContent).toContain('画像を除外しました。');
    await waitFor(() => expect(document.activeElement).toBe(origin));
    await userEvent.click(origin); expect(within(screen.getByRole('dialog')).getByText('バックアップは保存されています。')).toBeTruthy();
    expect(vi.mocked(SelectAndStartBackup)).toHaveBeenCalledTimes(1);
});

it('バックアップ中は別dialogを開かず、進行中処理は初回観測から再接続する', async () => {
    vi.mocked(GetBackupStatus).mockResolvedValue(main.BackupStatusDTO.createFrom({...idle, executionId: '1', state: 'running', stage: 'database', canCancel: true}));
    render(<App />); const dialog = await screen.findByRole('dialog', {name: 'バックアップ'});
    fireEvent.click(screen.getByRole('button', {name: 'タグ管理'}));
    expect(screen.getAllByRole('dialog')).toHaveLength(1);
    fireEvent(dialog, new Event('cancel', {cancelable: true})); expect(screen.getByRole('dialog')).toBe(dialog);
    expect(vi.mocked(SelectAndStartBackup)).not.toHaveBeenCalled();
});
