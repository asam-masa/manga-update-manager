import {expect, it} from 'vitest';
import {readLibrarySnapshot, libraryWorkView} from './library-snapshot';
import {readTagDeletePreview} from './tag-management';

const fixture = () => ({
    works: [{id: '1', url: 'https://example.com/1', title: '作品', siteName: '合成', thumbnailPath: 'covers/sample.png', notes: 'メモ',
        createdAt: '2026-11-01T10:00:00.000000001+09:00', updatedAt: '2026-11-01T10:00:00.123456789+09:00',
        lastAccessedAt: null as string | null, scheduledDate: null as string | null}],
    tags: [{id: '1', name: '共有'}, {id: '2', name: '未使用'}], links: [{workID: '1', tagID: '1'}],
});

it.each([
    {url: 'HTTPS://example.com/1', title: '作品'},
    {url: 'https://example.com/1', title: '\uFEFF'},
    {url: 'https://example.com:65536/1', title: '作品'},
])('Goが許可したスキーム・タイトルを正規化せず保持する (%j)', ({url, title}) => {
    const value = fixture(); value.works[0].url = url; value.works[0].title = title;
    expect(readLibrarySnapshot(value).works[0]).toEqual(value.works[0]);
});

it('一式の全フィールドと未使用タグ・空配列を保ち、入力を変更しない', () => {
    const value = fixture(); value.works[0].scheduledDate = '2028-02-29';
    value.works[0].lastAccessedAt = '2026-11-01T10:00:00.123456789+09:00';
    Object.values(value).forEach((array) => { array.forEach(Object.freeze); Object.freeze(array); }); Object.freeze(value);
    const got = readLibrarySnapshot(value);
    expect(got).toEqual(value);
    expect(got.works[0]).not.toBe(value.works[0]);
    expect(got.tags[0]).not.toBe(value.tags[0]);
    expect(got.links[0]).not.toBe(value.links[0]);
    expect(readLibrarySnapshot({works: [], tags: [], links: []})).toEqual({works: [], tags: [], links: []});
    expect(libraryWorkView(got.works[0])).toEqual({...got.works[0], id: 1});
});

it.each([undefined, null, [], {}, {works: []}, {works: [], tags: []}, {works: [], tags: [], links: null}])('未取得・部分値を空成功へ置換しない (%j)', (value) => {
    expect(() => readLibrarySnapshot(value)).toThrow();
});

it('不正な必須フィールド・ID・重複・参照・順序では一部だけ返さない', () => {
    const changes: ((value: any) => void)[] = [
        (v) => {v.works[0] = null;}, (v) => {v.works[0].id = 1;}, (v) => {v.works[0].id = '01';},
        (v) => {v.works[0].id = '9223372036854775808';}, (v) => {v.works[0].title = '';},
        (v) => {v.works[0].notes = '\ud800';}, (v) => {delete v.works[0].siteName;},
        (v) => {v.works[0].url = 'file:///private';}, (v) => {v.works[0].url = 'https://example.com\n';},
        (v) => {v.works[0].url = 'https:///missing-host';}, (v) => {v.works[0].url = 'https://user@/';},
        (v) => {v.works[0].thumbnailPath = '../private';},
        (v) => {v.works.push({...v.works[0]});},
        (v) => {v.works.unshift({...v.works[0], id: '2', url: 'https://example.com/2'});},
        (v) => {v.works.push({...v.works[0], id: '2'});},
        (v) => {v.tags[1].id = '1';}, (v) => {v.tags[1].name = v.tags[0].name;},
        (v) => {v.tags[1].name = ' 未正規化 ';}, (v) => {v.tags.reverse();},
        (v) => {v.links[0].workID = '9';}, (v) => {v.links[0].tagID = '9';},
        (v) => {v.links[0].tagID = 1;}, (v) => {v.links.push({...v.links[0]});},
        (v) => {v.links.unshift({workID: '1', tagID: '2'});},
        (v) => {v.works = new Array(1);}, (v) => {v.links = new Array(1);},
    ];
    for (const change of changes) { const value = fixture(); change(value); expect(() => readLibrarySnapshot(value)).toThrow(); }
});

it.each(['2028-02-30T00:00:00+09:00', '0000-01-01T00:00:00+09:00', '2026-11-01T24:00:00+09:00',
    '2026-11-01T10:00:00.1234567891+09:00', '2026-11-01T10:00:00.100+09:00', '0001-01-01T09:00:00+09:00'])('不正・非正規・ゼロ日時を拒否する (%s)', (date) => {
    const value = fixture(); value.works[0].createdAt = date;
    expect(() => readLibrarySnapshot(value)).toThrow();
});

it('日時の1ナノ秒差とオフセットを比較し、文字列は変換しない', () => {
    const value = fixture();
    value.works[0].updatedAt = '2026-11-01T01:00:00Z';
    expect(() => readLibrarySnapshot(value)).toThrow(); // One nanosecond before createdAt.
    value.works[0].updatedAt = '2026-11-01T01:00:00.000000001Z';
    expect(readLibrarySnapshot(value).works[0].updatedAt).toBe(value.works[0].updatedAt);
    value.works[0].createdAt = '1949-07-01T10:00:00.123456789+10:00';
    expect(readLibrarySnapshot(value).works[0].createdAt).toBe(value.works[0].createdAt);
    expect(readTagDeletePreview({tag: {id: '1', name: 'タグ'}, works: [{id: '1', title: '作品', createdAt: value.works[0].createdAt}]}, '1').works[0].createdAt).toBe(value.works[0].createdAt);
});

it.each(['', '2027-02-29', '2028-2-29', '0000-01-01', '10000-01-01', undefined, 1])('不正な予定日と未取得null項目を拒否する (%j)', (date) => {
    const value: any = fixture(); value.works[0].scheduledDate = date;
    expect(() => readLibrarySnapshot(value)).toThrow();
    const missing: any = fixture(); delete missing.works[0].lastAccessedAt;
    expect(() => readLibrarySnapshot(missing)).toThrow();
});

it.each(['0001-01-01', '2028-02-29', '9999-12-31'])('予定日は有効な暦日文字列をそのまま保持する (%s)', (date) => {
    const value = fixture(); value.works[0].scheduledDate = date;
    expect(readLibrarySnapshot(value).works[0].scheduledDate).toBe(date);
});

it.each(['9007199254740991', '9007199254740992', '9007199254740993', '9223372036854775807'])('巨大IDを文字列で保持し、安全に表現できる場合だけ数値へ変換する (%s)', (id) => {
    const value = fixture(); value.works[0].id = id; value.tags = [{id, name: 'タグ'}]; value.links = [{workID: id, tagID: id}];
    const got = readLibrarySnapshot(value);
    expect(got.works[0].id).toBe(id); expect(got.tags[0].id).toBe(id); expect(got.links[0]).toEqual({workID: id, tagID: id});
    if (id === '9007199254740991') expect(libraryWorkView(got.works[0]).id).toBe(Number.MAX_SAFE_INTEGER);
    else expect(() => libraryWorkView(got.works[0])).toThrow('Work ID cannot be represented safely');
});
