import {describe, expect, it} from 'vitest';
import {apiErrorMessage, isDatabaseStopped, registrationDate, scheduledDateLabel} from './work-display';

describe('表示境界', () => {
    it.each(['0001-01-01', '2028-02-29', '9999-12-31'])('予定日%sを暦日のまま表示する', (value) => {
        expect(scheduledDateLabel(value)).toBe(value.replaceAll('-', '/'));
    });
    it.each([undefined, null, ''])('予定日未設定%sを表示する', (value) => {
        expect(scheduledDateLabel(value)).toBe('未設定');
    });
    it.each([123, false, {}])('予定日の不正型%sで描画を壊さない', (value) => {
        expect(scheduledDateLabel(value)).toBe('日付を確認できません');
    });
    it.each(['work_changed', 'work_delete_outcome_unknown'])('削除APIのエラー %s を包装後も判別する', (code) => {
        const error = new Error('manga-update-manager:api-error:v1:' + JSON.stringify({code, message: '削除結果を確認してください。'}));
        expect(apiErrorMessage(error, '案内')).toBe('削除結果を確認してください。');
        expect(isDatabaseStopped(error)).toBe(code === 'work_delete_outcome_unknown');
    });
    it.each(['work_busy', 'work_edit_outcome_unknown'])('編集APIのエラー %s を包装後も判別する', (code) => {
        const error = new Error('manga-update-manager:api-error:v1:' + JSON.stringify({code, message: '保存結果を確認してください。'}));
        expect(apiErrorMessage(error, '案内')).toBe('保存結果を確認してください。');
        expect(isDatabaseStopped(error)).toBe(code === 'work_edit_outcome_unknown');
    });
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
