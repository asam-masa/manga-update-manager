import {act, fireEvent, render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {WorkList} from './WorkList';
import type {WorkView} from './work-display';

const work: WorkView = {id: 1, url: 'https://example.com/very-long-url', title: '作品', siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-01T00:00:00+09:00', updatedAt: '2026-10-01T00:00:00+09:00', lastAccessedAt: null};

describe('作品URL操作', () => {
    it('未アクセスと全文titleを表示し、キーボードで作品IDを渡す', async () => {
        const open = vi.fn().mockResolvedValue(undefined);
        render(<WorkList works={[work]} showEmpty onOpen={open} />);
        expect(screen.getByText(/未アクセス/)).toBeTruthy();
        const link = screen.getByRole('button', {name: '作品のURLを開く'});
        expect(link.getAttribute('title')).toBe(work.url);
        link.focus();
        await userEvent.keyboard('{Enter}');
        expect(open).toHaveBeenCalledExactlyOnceWith(1);
        expect(screen.getByRole('heading').tagName).toBe('H3');
    });
    it.each(['browser_open_failed', 'access_save_failed'])('失敗 %s で既存日時を残す', async (code) => {
        render(<WorkList works={[{...work, lastAccessedAt: '2026-10-08T12:34:56+09:00'}]} showEmpty
            onOpen={vi.fn().mockRejectedValue({code, message: '起動または保存を確認してください'})} />);
        await userEvent.click(screen.getByRole('button'));
        expect((await screen.findByRole('alert')).textContent).toContain('確認してください');
        expect(screen.getByText('2026/10/08 12:34:56')).toBeTruthy();
    });
    it('処理中は同じ作品を二重起動しない', async () => {
        let done!: () => void;
        const open = vi.fn(() => new Promise<void>((resolve) => { done = resolve; }));
        render(<WorkList works={[work]} showEmpty onOpen={open} />);
        fireEvent.click(screen.getByRole('button'));
        fireEvent.click(screen.getByRole('button'));
        expect(open).toHaveBeenCalledTimes(1);
        expect((screen.getByRole('button') as HTMLButtonElement).disabled).toBe(true);
        await act(async () => { done(); });
        expect((screen.getByRole('button') as HTMLButtonElement).disabled).toBe(false);
    });
});
