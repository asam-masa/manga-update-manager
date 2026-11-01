import {expect, it} from 'vitest';
import {main} from '../wailsjs/go/models';
import {apiErrorCode, isDatabaseStopped} from './work-display';

it('一括取得の生成型がID全桁・日時・予定日とnullをJSON往復で保持する', () => {
    const id = '9223372036854775807';
    const work = {id, url: 'https://example.com', title: '作品', siteName: '', thumbnailPath: '', notes: '', createdAt: '2026-11-01T10:02:03.123456789+09:00', updatedAt: '2026-11-01T10:02:03.123456789+09:00', lastAccessedAt: null, scheduledDate: '2028-02-29'};
    const source = {works: [work], tags: [{id, name: 'タグ'}], links: [{workID: id, tagID: id}]};
    expect(JSON.parse(JSON.stringify(main.LibrarySnapshotDTO.createFrom(JSON.stringify(source))))).toEqual(source);
    const empty = {works: [], tags: [], links: []};
    expect(JSON.parse(JSON.stringify(main.LibrarySnapshotDTO.createFrom(empty)))).toEqual(empty);
});

it('既存の数値WorkDTOと既知の読取エラーの包装を維持する', () => {
    expect(main.WorkDTO.createFrom({id: 1}).id).toBe(1);
    for (const code of ['storage_error', 'database_unavailable', 'database_stopped']) {
        const error = new Error('manga-update-manager:api-error:v1:' + JSON.stringify({code, message: '再読み込みしてください'}));
        expect(apiErrorCode(error)).toBe(code);
        expect(isDatabaseStopped(error)).toBe(code === 'database_stopped');
    }
});
