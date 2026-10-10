import {describe, expect, it} from 'vitest';
import {titleMatches} from './work-search';

describe('タイトル部分一致', () => {
    it.each([
        ['ＡＢＣ物語', ' abc ', true], ['ABC物語', '　ａｂｃ　', true],
        ['ガンダム', 'ｶﾞﾝ', true], ['まんが', 'マンガ', false],
        ['A Bの物語', 'a b', true], ['A Bの物語', 'ab', false],
        ['A　B', 'a b', true], ['①巻', '1', false], ['物語', '　 ', true],
        ['作品名', '作品名より長い検索', false], ['ヴァイオリン', 'ウ\u3099ァ', true],
    ])('%s と %s の一致は %s', (title, query, matches) => {
        expect(titleMatches(title, query)).toBe(matches);
    });
});
