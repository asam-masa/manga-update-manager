import {describe, expect, it} from 'vitest';
import {filterLibrary} from './tag-filter';
import type {TagFilterConditions} from './tag-filter';

const tags = [{id: '1', name: 'ファンタジー'}, {id: '2', name: '毎週更新'}, {id: '3', name: '毎月更新'}, {id: '4', name: '未使用'}];
const works = [
    {id: '1', title: 'マンガ物語1', scheduledDate: '2026-11-01', notes: '保持する'},
    {id: '2', title: 'マンガ物語2', scheduledDate: '2026-11-02', notes: '未来'},
    {id: '3', title: 'マンガ物語3', scheduledDate: '2026-10-31', notes: '過去'},
    {id: '4', title: '別の作品', scheduledDate: null, notes: '予定なし'},
    {id: '5', title: 'タグなし', scheduledDate: undefined, notes: '未設定'},
];
const links = [{workID: '1', tagID: '1'}, {workID: '1', tagID: '2'}, {workID: '2', tagID: '1'},
    {workID: '2', tagID: '3'}, {workID: '3', tagID: '2'}, {workID: '4', tagID: '1'}, {workID: '4', tagID: '2'}];
const snapshot = {works, tags, links};
const conditions: TagFilterConditions = {selectedTagIDs: [], titleQuery: '', candidateQuery: '', order: 'registration_asc', today: '2026-11-01', showFuture: true};
const result = (override: Partial<TagFilterConditions> = {}) => filterLibrary(snapshot, {...conditions, ...override});
const ids = (override: Partial<TagFilterConditions> = {}) => result(override).works.map((work) => work.id);
const counts = (override: Partial<TagFilterConditions> = {}) => Object.fromEntries(result(override).candidates.map(({tag, workCount}) => [tag.id, workCount]));

describe('作品のANDと候補追加後の件数', () => {
    it('未選択ならタグなしも含め、候補は全作品の使用数になる', () => {
        expect(ids()).toEqual(['1', '2', '3', '4', '5']);
        expect(counts()).toEqual({'1': 3, '2': 3, '3': 1, '4': 0});
    });
    it('二つのタグを両方持つ作品だけを残し、選択候補は現在件数になる', () => {
        expect(ids({selectedTagIDs: ['1', '2']})).toEqual(['1', '4']);
        expect(counts({selectedTagIDs: ['1', '2']})).toEqual({'1': 2, '2': 2, '3': 0, '4': 0});
    });
    it('重複選択で条件や件数を増やさない', () => {
        expect(result({selectedTagIDs: ['1', '1', '2']})).toEqual(result({selectedTagIDs: ['1', '2']}));
    });
    it('追加すると0件の候補や未使用タグも残し、選べば正当な0件になる', () => {
        expect(ids({selectedTagIDs: ['1', '2', '3']})).toEqual([]);
        expect(ids({selectedTagIDs: ['4']})).toEqual([]);
        expect(counts({selectedTagIDs: ['4']})).toEqual({'1': 0, '2': 0, '3': 0, '4': 0});
    });
    it('未知・削除済みの選択を勝手に解除せず呼出し側の再確認へ返す', () => {
        expect(() => result({selectedTagIDs: ['99']})).toThrow('Unknown selected tag');
    });
    it('タイトル検索・予定条件・選択タグを合わせた件数を返す', () => {
        const condition = {selectedTagIDs: ['1'], titleQuery: 'ﾏﾝｶﾞ', order: 'scheduled_date' as const, showFuture: false};
        expect(ids(condition)).toEqual(['1']);
        expect(counts(condition)).toEqual({'1': 1, '2': 1, '3': 0, '4': 0});
    });
    it('候補名の検索では作品を絞らず、候補に表示する件数だけ取り出す', () => {
        expect(ids({candidateQuery: '毎週'})).toEqual(ids());
        expect(counts({candidateQuery: '毎週'})).toEqual({'2': 3});
        expect(result({candidateQuery: '存在しない'}).candidates).toEqual([]);
    });
});

describe('予定と順序と検索', () => {
    it.each(['registration_asc', 'registration_desc', 'title_asc', 'title_desc', 'last_access_asc', 'last_access_desc'] as const)('%sは未来OFFでも未来を除外しない', (order) => {
        expect(ids({order, showFuture: false})).toEqual(['1', '2', '3', '4', '5']);
    });
    it('予定順でだけ未来を除外し、今日・過去・未設定は残す。整列は後段へ委ねる', () => {
        expect(ids({order: 'scheduled_date', showFuture: false})).toEqual(['1', '3', '4', '5']);
        expect(ids({order: 'scheduled_date', showFuture: true})).toEqual(['1', '2', '3', '4', '5']);
    });
    it('既存検索と同じ表記差だけを吸収し、保存名や検索対象を変更しない', () => {
        const data = {works: [{id: '1', title: 'ＡＢＣ マンガ①'}], tags: [{id: '1', name: 'ＡＢＣ マンガ①'}], links: [{workID: '1', tagID: '1'}]};
        const found = filterLibrary(data, {...conditions, titleQuery: ' abc ﾏﾝｶﾞ ', candidateQuery: 'abc ﾏﾝｶﾞ'});
        expect(found.works).toEqual(data.works);
        expect(found.candidates[0]).toEqual({tag: data.tags[0], workCount: 1});
        expect(filterLibrary(data, {...conditions, titleQuery: 'マンガ1'}).works).toEqual([]);
    });
    it('件数や候補検索で自然順を変えず、同順位は巨大IDも文字列で比べる', () => {
        const data = {works: [{id: '9223372036854775807', title: '作品'}], tags: [
            {id: '1', name: 'シリーズ10'}, {id: '9007199254740992', name: 'シリーズ002'},
            {id: '9007199254740993', name: 'シリーズ02'}, {id: '9223372036854775807', name: 'シリーズ2'}],
        links: [{workID: '9223372036854775807', tagID: '9223372036854775807'}]};
        const before = filterLibrary(data, conditions);
        expect(before.candidates.map((c) => c.tag.id)).toEqual(['9007199254740992', '9007199254740993', '9223372036854775807', '1']);
        const after = filterLibrary(data, {...conditions, selectedTagIDs: ['9223372036854775807']});
        expect(after.works[0].id).toBe('9223372036854775807');
        expect(after.candidates.map((c) => c.tag.id)).toEqual(before.candidates.map((c) => c.tag.id));
    });
});

describe('全件計算と入力保持', () => {
    it('ページをまたぐ一致も集計し、作品オブジェクト・入力順・全フィールドを維持する', () => {
        const all = Array.from({length: 41}, (_, i) => ({id: String(i + 1), title: `作品${i + 1}`, notes: `メモ${i}`, scheduledDate: null}));
        const data = {works: all, tags: [{id: '1', name: '全作品'}], links: all.map((w) => ({workID: w.id, tagID: '1'}))};
        const got = filterLibrary(data, {...conditions, selectedTagIDs: ['1']});
        expect(got.works).toEqual(all);
        expect(got.works[40]).toBe(all[40]);
        expect(got.works[40].notes).toBe('メモ40');
        expect(got.candidates[0].workCount).toBe(41);
    });
    it('凍結した入力を変更せず、タグなし・作品なし・候補なしにも対応する', () => {
        const frozen = {works: Object.freeze(works.map((w) => Object.freeze({...w}))), tags: Object.freeze(tags.map((t) => Object.freeze({...t}))), links: Object.freeze(links.map((l) => Object.freeze({...l})))};
        const options = Object.freeze({...conditions, selectedTagIDs: Object.freeze(['1'])});
        expect(filterLibrary(frozen, options).works.map((w) => w.id)).toEqual(['1', '2', '4']);
        expect(filterLibrary({works: [], tags, links: []}, conditions).candidates.every((c) => c.workCount === 0)).toBe(true);
        expect(filterLibrary({works: [], tags: [], links: []}, conditions)).toEqual({works: [], candidates: []});
        expect(filterLibrary({works, tags: [], links: []}, conditions).works).toEqual(works);
    });
});
