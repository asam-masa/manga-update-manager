import {fireEvent, render, screen} from '@testing-library/react';
import {expect, it, vi} from 'vitest';
import {WorkList} from './WorkList';
import type {WorkView} from './work-display';

const work: WorkView = {id: 1, title: '作品', url: 'https://example.com/', siteName: '', notes: '',
    thumbnailPath: 'covers/private-name.png', createdAt: '2026-10-19T00:00:00+09:00', updatedAt: '2026-10-19T00:00:00+09:00'};
const onOpen = vi.fn();

it('整理完了前と画像未設定では取得せず、管理パスをURLに含めない', () => {
    const view = render(<WorkList works={[work]} showEmpty={false} onOpen={onOpen} coversReady={false} coverRevision={0} />);
    expect(view.container.querySelector('img')).toBeNull();
    view.rerender(<WorkList works={[work]} showEmpty={false} onOpen={onOpen} coversReady coverRevision={0} />);
    const image = view.container.querySelector('img')!;
    expect(image.getAttribute('src')).toMatch(/^\/__covers\/works\/1\?v=[0-9a-f]{32}$/);
    expect(image.getAttribute('alt')).toBe('');
    expect(image.className).toBe('cover-loading');
    fireEvent.load(image);
    expect(image.className).toBe('');
    expect(screen.queryByText('本')).toBeNull();
    view.rerender(<WorkList works={[{...work, thumbnailPath: ''}]} showEmpty={false} onOpen={onOpen} coversReady coverRevision={0} />);
    expect(view.container.querySelector('img')).toBeNull();
    expect(screen.queryByText('画像を表示できません')).toBeNull();
});

it('画像取得失敗は当該カード内だけに表示し、再読込で画像と失敗状態を作り直す', () => {
    const props = {works: [work, {...work, id: 2, title: '別作品'}], showEmpty: false, onOpen, coversReady: true};
    const view = render(<WorkList {...props} coverRevision={0} />);
    const image = view.container.querySelector('img')!;
    fireEvent.error(image);
    expect(screen.getAllByText('画像を表示できません')).toHaveLength(1);
    expect(view.container.querySelectorAll('img')).toHaveLength(1);
    expect(screen.getByRole('button', {name: '作品のURLを開く'})).toBeTruthy();
    view.rerender(<WorkList {...props} coverRevision={1} />);
    expect(screen.queryByText('画像を表示できません')).toBeNull();
    expect(view.container.querySelectorAll('img')).toHaveLength(2);
    expect(view.container.querySelector('img')).not.toBe(image);
    expect(view.container.querySelector('img')!.getAttribute('src')).not.toBe(image.getAttribute('src'));
    fireEvent.error(image); // Detached prior element cannot replace the new state.
    expect(screen.queryByText('画像を表示できません')).toBeNull();
});

it('通常の再描画ではURLを保ち、画像変更では同じ作品でもURLを変える', () => {
    const props = {showEmpty: false, onOpen, coversReady: true, coverRevision: 0};
    const view = render(<WorkList {...props} works={[work]} />);
    const source = view.container.querySelector('img')!.getAttribute('src');
    view.rerender(<WorkList {...props} works={[{...work, lastAccessedAt: '2026-10-19T01:00:00+09:00'}]} />);
    expect(view.container.querySelector('img')!.getAttribute('src')).toBe(source);
    view.rerender(<WorkList {...props} works={[{...work, thumbnailPath: 'covers/another-private-name.png'}]} />);
    const next = view.container.querySelector('img')!.getAttribute('src');
    expect(next).toMatch(/^\/__covers\/works\/1\?v=[0-9a-f]{32}$/);
    expect(next).not.toBe(source);
});
