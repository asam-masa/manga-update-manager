import {describe, expect, it} from 'vitest';
import {apiErrorMessage, registrationDate} from './work-display';

describe('表示境界', () => {
    it('ホストのタイムゾーンに依存せず日本時間へ変換する', () => {
        expect(registrationDate('2026-10-05T18:30:00Z')).toBe('2026/10/06 03:30:00');
        expect(registrationDate('invalid')).toBe('日時を確認できません');
    });
    it('未知のエラーや内部診断をそのまま表示しない', () => {
        for (const error of [null, 'private path', new Error('private path'), {code: 'unknown', message: 'private'}, {code: 'invalid_input', message: ''}]) {
            expect(apiErrorMessage(error, '案内')).toBe('案内');
        }
        expect(apiErrorMessage({code: 'duplicate_url', message: '登録済みです'}, '案内')).toBe('登録済みです');
    });
});
