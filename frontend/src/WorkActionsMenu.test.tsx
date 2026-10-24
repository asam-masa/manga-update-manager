import {fireEvent, render, screen, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {expect, it, vi} from 'vitest';
import {WorkList} from './WorkList';

const work = {id: 1, title: '一', url: 'https://example.com', siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-24T00:00:00Z', updatedAt: '2026-10-24T00:00:00Z'};
function setup() {
    const editing = {onEdit: vi.fn(), stopped: false};
    const deleting = {onDelete: vi.fn(), stopped: false};
    const props = {works: [work, {...work, id: 2, title: '二'}], showEmpty: true, onOpen: vi.fn().mockResolvedValue(undefined), coversReady: true, coverRevision: 0, editing, deleting};
    const view = render(<WorkList {...props} />);
    return {...view, props};
}
const trigger = (title = '一') => screen.getByRole('button', {name: `${title}の作品操作`});
const panel = (title = '一') => screen.getByRole('group', {name: `${title}の作品操作一覧`});

it('上下/Home/Endで移動し、Enterは既存操作へ元の⋯を渡して閉じる', async () => {
    const {props} = setup();
    trigger().focus();
    await userEvent.keyboard('{ArrowUp}');
    expect(document.activeElement).toBe(within(panel()).getByRole('button', {name: '一を削除'}));
    await userEvent.keyboard('{ArrowDown}');
    expect(document.activeElement).toBe(within(panel()).getByRole('button', {name: '一を編集'}));
    await userEvent.keyboard('{End}{Home}{Enter}');
    expect(props.editing.onEdit).toHaveBeenCalledExactlyOnceWith(work, trigger());
    expect(screen.queryByRole('group', {name: '一の作品操作一覧'})).toBeNull();
});

it('別の作品を開くと一つだけになり、Escは元ボタンへ戻る', async () => {
    setup();
    await userEvent.click(trigger());
    await userEvent.click(trigger('二'));
    expect(screen.queryByRole('group', {name: '一の作品操作一覧'})).toBeNull();
    expect(panel('二')).toBeTruthy();
    await userEvent.keyboard('{Escape}');
    expect(document.activeElement).toBe(trigger('二'));
    expect(trigger('二').getAttribute('aria-expanded')).toBe('false');
});

it('外クリック・Tabで移ったフォーカスを奪わず、検索非表示から戻っても閉じたまま', async () => {
    const {props, rerender} = setup();
    await userEvent.click(trigger());
    const url = screen.getByRole('button', {name: '二のURLを開く'});
    await userEvent.click(url);
    expect(document.activeElement).toBe(url);
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    await userEvent.click(trigger());
    await userEvent.keyboard('{End}{Tab}');
    expect(document.activeElement).toBe(url);
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
    await userEvent.click(trigger());
    rerender(<WorkList {...props} works={[]} />);
    rerender(<WorkList {...props} />);
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
});

it('画面端ではviewport内へ寄せ、パネル内スクロールは維持し外側の移動で閉じる', async () => {
    const rect = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function(this: HTMLElement) {
        return this.classList.contains('work-actions-panel') ? {width: 200, height: 180} as DOMRect
            : {right: innerWidth - 5, bottom: innerHeight - 5, top: innerHeight - 45} as DOMRect;
    });
    try {
        setup();
        await userEvent.click(trigger());
        expect(Number.parseFloat(panel().style.left)).toBeLessThanOrEqual(innerWidth - 208);
        expect(Number.parseFloat(panel().style.top)).toBeGreaterThanOrEqual(8);
        expect(Number.parseFloat(panel().style.top) + 180).toBeLessThanOrEqual(innerHeight - 8);
        fireEvent.scroll(panel());
        expect(panel()).toBeTruthy();
        fireEvent.scroll(window);
        expect(trigger().getAttribute('aria-expanded')).toBe('false');
    } finally { rect.mockRestore(); }
});

it('すべて無効でも⋯は開けてEscで戻れる', async () => {
    const {props, rerender} = setup();
    rerender(<WorkList {...props} isOpening={() => true} />);
    await userEvent.click(trigger());
    expect(within(panel()).getAllByRole('button').every((button) => (button as HTMLButtonElement).disabled)).toBe(true);
    expect(document.activeElement).toBe(trigger());
    await userEvent.keyboard('{Escape}');
    expect(document.activeElement).toBe(trigger());
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
});
