import {describe, expect, it} from 'vitest';
import {apiErrorMessage, isDatabaseStopped, registrationDate} from './work-display';

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
    it('WailsのError包装後も安全なJSON文字列だけを復元する', () => {
        const wrapped = new Error('manga-update-manager:api-error:v1:' + JSON.stringify({code: 'database_stopped', message: '再起動してください。'}));
        expect(apiErrorMessage(wrapped, '案内')).toBe('再起動してください。');
        expect(isDatabaseStopped(wrapped)).toBe(true);
        const prefix = 'manga-update-manager:api-error:v1:';
        for (const error of [new Error('[object Object]'), new Error('{"code":"database_stopped","message":"private"}'),
            new Error(prefix + '{'), new Error(prefix + 'null'), new Error(prefix + '[]'),
            new Error(prefix + '{"code":"unknown","message":"private"}'), new Error(prefix + '{"code":"database_stopped"}'),
            new Error(prefix + '{"code":"database_stopped","message":"  "}'),
            new Error(prefix + 'x'.repeat(8192)), new Error(prefix.replace('v1:', 'v2:') + '{}'),
            prefix + '{"code":"database_stopped","message":"private"}']) {
            expect(apiErrorMessage(error, '案内')).toBe('案内');
            expect(isDatabaseStopped(error)).toBe(false);
        }
    });
});
