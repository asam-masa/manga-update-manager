import {act, fireEvent, render, screen, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {expect, it, vi} from 'vitest';
import {WorkList} from './WorkList';
import type {WorkListMode} from './WorkList';

const work = {id: 1, title: '長いタイトルの漫画物語', url: 'https://example.com/long/registered-url', siteName: '漫画サイト', notes: '一覧には表示しないメモ', thumbnailPath: 'covers/sample.png', createdAt: '2026-10-01T00:00:00Z', updatedAt: '2026-10-01T00:00:00Z', lastAccessedAt: null};
const props = () => ({works: [work], showEmpty: true, onOpen: vi.fn().mockResolvedValue(undefined), coversReady: true, coverRevision: 0, editing: {onEdit: vi.fn(), stopped: false}});

it.each<WorkListMode>(['compact', 'rows', 'cards'])('%sの予定日表示を設定・未設定・解除で切り替える', (mode) => {
    const values = props(); const dated = {...work, scheduledDate: '2028-02-29'};
    const view = render(<WorkList {...values} works={[dated]} mode={mode} />);
    if (mode === 'compact') {
        expect(view.container.querySelector('.work-scheduled-date')).toBeNull();
    } else {
        const date = view.container.querySelector('.work-scheduled-date')!;
        expect(date.textContent).toBe(mode === 'cards' ? '更新予定 2028/02/29' : '2028/02/29');
        expect(date.querySelector('time')?.getAttribute('datetime')).toBe('2028-02-29');
        expect(date.getAttribute('title')).toBe('更新予定 2028/02/29');
        view.rerender(<WorkList {...values} works={[{...work, scheduledDate: null}]} mode={mode} />);
        expect(view.container.querySelector('.work-scheduled-date')?.textContent).toBe(mode === 'cards' ? '更新予定 未設定' : '未設定');
        expect(view.container.querySelector('.work-scheduled-date time')).toBeNull();
        view.rerender(<WorkList {...values} works={[{...work, scheduledDate: '0001-01-01'}]} mode={mode} />);
        expect(view.container.querySelector('.work-scheduled-date')?.textContent).toContain('0001/01/01');
    }
    expect(values.onOpen).not.toHaveBeenCalled();
});

it.each<WorkListMode>(['compact', 'rows', 'cards'])('%sは起動入口が一つ、タイトルはリンクにせず全文titleを持つ', async (mode) => {
    const values = props();
    render(<WorkList {...values} mode={mode} />);
    const title = screen.getByRole('heading', {name: work.title});
    expect(title.getAttribute('title')).toBe(work.title);
    expect(title.closest('a, button')).toBeNull();
    const entry = screen.getByRole('button', {name: `${work.title}のURLを開く`});
    expect(entry.getAttribute('title')).toBe(work.url);
    expect(screen.getAllByRole('button', {name: /のURLを開く/})).toHaveLength(1);
    expect(screen.queryByText(work.notes)).toBeNull();
    expect(screen.queryByText(/登録 /)).toBeNull();
    entry.focus(); await userEvent.keyboard('{Enter}');
    expect(values.onOpen).toHaveBeenCalledExactlyOnceWith(work.id);
    if (mode === 'compact') expect(entry.querySelector('img')).not.toBeNull();
    if (mode === 'rows') {
        expect(screen.getAllByRole('columnheader').map((header) => header.textContent)).toEqual(['タイトル', 'URL', '最終アクセス', '更新予定日', '操作']);
        expect(screen.getByRole('table').querySelector('img')).toBeNull();
        expect(screen.queryByText(work.siteName)).toBeNull();
    }
    if (mode === 'cards') expect(screen.getByText(work.siteName)).toBeTruthy();
});

it.each(['missing', 'failed'])('小型の画像が%sでも同じ入口を維持する', async (state) => {
    const values = props();
    const view = render(<WorkList {...values} works={[{...work, thumbnailPath: state === 'missing' ? '' : work.thumbnailPath}]} mode="compact" />);
    const entry = screen.getByRole('button', {name: `${work.title}のURLを開く`});
    if (state === 'failed') fireEvent.error(view.container.querySelector('img')!);
    expect(within(entry).getByText('本')).toBeTruthy();
    await userEvent.click(entry);
    expect(values.onOpen).toHaveBeenCalledExactlyOnceWith(work.id);
});

it.each<WorkListMode>(['compact', 'rows', 'cards'])('%sでもAppの処理中状態で入口・編集を無効にする', async (mode) => {
    const values = props();
    render(<WorkList {...values} mode={mode} isOpening={() => true} />);
    const entry = screen.getByRole('button', {name: `${work.title}のURLを開く`});
    expect((entry as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(entry);
    expect(values.onOpen).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', {name: `${work.title}の作品操作`}));
    expect((screen.getByRole('button', {name: `${work.title}を編集`}) as HTMLButtonElement).disabled).toBe(true);
});

it('形式変更はパネルを閉じ、切替先のフォーカスを奪わず、戻しても再び開かない', async () => {
    const values = props();
    const view = render(<><button>形式を切替</button><WorkList {...values} mode="cards" /></>);
    await userEvent.click(screen.getByRole('button', {name: `${work.title}の作品操作`}));
    view.rerender(<><button>形式を切替</button><WorkList {...values} mode="rows" /></>);
    expect(screen.queryByRole('group', {name: /作品操作一覧/})).toBeNull();
    const outside = screen.getByRole('button', {name: '形式を切替'});
    outside.focus();
    view.rerender(<><button>形式を切替</button><WorkList {...values} mode="cards" /></>);
    expect(document.activeElement).toBe(outside);
    expect(screen.queryByRole('group', {name: /作品操作一覧/})).toBeNull();
});

it('一覧の実幅を観測し、フォーム表示相当の狭さで列数を減らし開いたパネルを閉じる', async () => {
    let resized!: ResizeObserverCallback;
    const disconnect = vi.fn();
    vi.stubGlobal('ResizeObserver', class { constructor(callback: ResizeObserverCallback) {resized = callback;} observe() {} disconnect = disconnect; });
    try {
        const view = render(<WorkList {...props()} mode="compact" />);
        const layout = view.container.querySelector<HTMLElement>('.work-layout')!;
        for (const [width, expected] of [[940, '5'], [800, '4'], [600, '2']]) {
            act(() => resized([{contentRect: {width}} as ResizeObserverEntry], {} as ResizeObserver));
            expect(layout.style.getPropertyValue('--compact-columns')).toBe(expected);
        }
        await userEvent.click(screen.getByRole('button', {name: `${work.title}の作品操作`}));
        expect(screen.getByRole('group', {name: /作品操作一覧/})).toBeTruthy();
        act(() => resized([{contentRect: {width: 940}} as ResizeObserverEntry], {} as ResizeObserver));
        expect(screen.queryByRole('group', {name: /作品操作一覧/})).toBeNull();
        view.unmount();
        expect(disconnect).toHaveBeenCalledTimes(1);
    } finally { vi.unstubAllGlobals(); }
});
