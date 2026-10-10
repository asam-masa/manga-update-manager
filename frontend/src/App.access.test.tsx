import {act, render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {expect, it, vi} from 'vitest';
import App from './App';
import {ListWorks, OpenWork} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({ListWorks: vi.fn(), OpenWork: vi.fn(), CreateWork: vi.fn(), LoadDisplaySettings: vi.fn().mockResolvedValue({registrationFormVisible: true}), SetRegistrationFormVisible: vi.fn()}));

it('一覧の古い応答でもアクセス日時を戻さず、追加作品は表示する', async () => {
    const work: main.WorkDTO = {id: 1, title: '作品', url: 'https://example.com', siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-01T00:00:00+09:00', updatedAt: '2026-10-01T00:00:00+09:00'};
    let reply!: (value: main.WorkDTO[]) => void;
    vi.mocked(ListWorks).mockResolvedValueOnce([work]).mockReturnValueOnce(new Promise((resolve) => { reply = resolve; }));
    vi.mocked(OpenWork).mockResolvedValue({...work, lastAccessedAt: '2026-10-09T12:00:00+09:00'});
    render(<App />);
    await screen.findByRole('heading', {name: '作品'});
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await userEvent.click(screen.getByRole('button', {name: '作品のURLを開く'}));
    await screen.findByText('2026/10/09 12:00:00');
    await act(async () => { reply([work, {...work, id: 2, title: '追加作品'}]); });
    expect(screen.getByText('2026/10/09 12:00:00')).toBeTruthy();
    expect(screen.getByRole('heading', {name: '追加作品'})).toBeTruthy();
});
