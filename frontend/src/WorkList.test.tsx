import {render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {describe, expect, it, vi} from 'vitest';
import {WorkList} from './WorkList';
import type {WorkView} from './work-display';

const work: WorkView = {id: 1, url: 'https://example.com/very-long-url', title: '作品', siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-01T00:00:00+09:00', updatedAt: '2026-10-01T00:00:00+09:00', lastAccessedAt: null};

describe('作品URL操作', () => {
    it('未アクセスと全文titleを表示し、キーボードで作品IDを渡す', async () => {
        const open = vi.fn().mockResolvedValue(undefined);
        render(<WorkList works={[work]} showEmpty onOpen={open} coversReady coverRevision={0} />);
        expect(screen.getByText(/未アクセス/)).toBeTruthy();
        const link = screen.getByRole('button', {name: '作品のURLを開く'});
        expect(link.getAttribute('title')).toBe(work.url);
        link.focus();
        await userEvent.keyboard('{Enter}');
        expect(open).toHaveBeenCalledExactlyOnceWith(1);
        expect(screen.getByRole('heading').tagName).toBe('H3');
    });
    it('Appから渡された処理状態でカード再表示直後もURLと編集・削除を無効にする', async () => {
        const props = {works: [work], showEmpty: true, onOpen: vi.fn(), coversReady: true, coverRevision: 0,
            editing: {onEdit: vi.fn(), stopped: false}, deleting: {onDelete: vi.fn(), stopped: false}};
        const view = render(<WorkList {...props} isOpening={() => true} />);
        await userEvent.click(screen.getByRole('button', {name: '作品の作品操作'}));
        expect(['作品のURLを開く', '作品を編集', '作品を削除'].every((name) => (screen.getByRole('button', {name}) as HTMLButtonElement).disabled)).toBe(true);
        expect(screen.queryByRole('status')).toBeNull();
        view.rerender(<WorkList {...props} isOpening={() => false} />);
        expect(screen.getAllByRole('button').every((button) => !(button as HTMLButtonElement).disabled)).toBe(true);
    });
});
