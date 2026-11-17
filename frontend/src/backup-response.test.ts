import {describe, expect, it} from 'vitest';
import {readBackupCancel, readBackupResult, readBackupStart, readBackupStatus, validBackupID} from './backup-response';
import {main} from '../wailsjs/go/models';

const id = '18446744073709551615';
const running = {executionId: id, state: 'running', stage: 'database', elapsedSeconds: 3, canCancel: true, result: null};
const observed = {publication: 'confirmed', hasWarnings: false, omittedImageCount: 0, workNamesState: 'available', workCount: 0};
const finished = {...running, state: 'finished', canCancel: false, result: observed};
const result = {executionId: id, outcome: 'saved', message: '保存しました。', cancellationObserved: false,
    retryAllowed: true, notices: [{code: 'cleanup_warning', message: '片付けを確認してください。'}]};

describe('バックアップ応答の実行ID', () => {
    it.each(['1', '9007199254740993', id])('精度を落とさず文字列を保持する: %s', (value) => {
        expect(readBackupStart({cancelled: false, executionId: value}).executionId).toBe(value);
    });
    it.each(['', '0', '01', '-1', '+1', ' 1', '1 ', '1.0', '1e2', '18446744073709551616', 1, null])('不正IDを拒否する: %s', (value) => {
        expect(validBackupID(value)).toBe(false);
        expect(() => readBackupStart({cancelled: false, executionId: value})).toThrow();
    });
    it('選択取消を開始受付と区別する', () => {
        expect(readBackupStart({cancelled: true, executionId: ''})).toEqual({cancelled: true, executionId: ''});
        expect(() => readBackupStart({cancelled: true, executionId: id})).toThrow();
        expect(() => readBackupStart({executionId: id})).toThrow();
    });
    it('別実行の結果・取消と不正な要求IDを拒否する', () => {
        expect(() => readBackupResult(result, '1')).toThrow();
        expect(() => readBackupCancel({executionId: id, decision: 'accepted'}, '1')).toThrow();
        expect(() => readBackupResult(result, '')).toThrow();
    });
    it.each([undefined, null, '', '0', '01'])('要求IDの欠落や不正表記で照合を省略しない: %s', (id) => {
        // Exercise the runtime boundary even when JS callers violate TS types.
        expect(() => readBackupResult(result, id as string)).toThrow();
        expect(() => readBackupCancel({executionId: result.executionId, decision: 'accepted'}, id as string)).toThrow();
    });
});

describe('状況と未取得の観測', () => {
    it('idleは開始可能性と区別した観測として返す', () => {
        expect(readBackupStatus({executionId: '', state: 'idle', stage: 'unobserved', elapsedSeconds: 0, canCancel: false})).toMatchObject({state: 'idle', result: null});
        expect(() => readBackupStatus({...running, state: 'idle'})).toThrow();
    });
    it.each(['waiting', 'preparing', 'database', 'database_check', 'images', 'metadata', 'zip', 'publishing', 'cleanup', 'unobserved'])('段階だけで取消権限を変えない: %s', (stage) => {
        expect(readBackupStatus({...running, stage}).canCancel).toBe(true);
    });
    it.each(['cancelling', 'publishing', 'unknown'])('処理継続中の状態を完了としない: %s', (state) => {
        expect(readBackupStatus({...running, state, canCancel: false}).result).toBeNull();
        expect(() => readBackupStatus({...running, state})).toThrow();
    });
    it.each([null, undefined])('null/undefined件数は未取得とし0へ変えない: %s', (value) => {
        const parsed = readBackupStatus({...finished, result: {...observed, omittedImageCount: value, workCount: value}});
        expect(parsed.result?.omittedImageCount).toBeNull();
        expect(parsed.result?.workCount).toBeNull();
    });
    it('生成クラスでも観測済み0件を保持する', () => {
        expect(readBackupStatus(main.BackupStatusDTO.createFrom(finished)).result).toEqual(observed);
    });
    it.each([-1, NaN, Infinity, 0.5, Number.MAX_SAFE_INTEGER + 1, '0'])('不正な秒・件数を拒否する: %s', (value) => {
        expect(() => readBackupStatus({...running, elapsedSeconds: value})).toThrow();
        expect(() => readBackupStatus({...finished, result: {...observed, workCount: value}})).toThrow();
    });
    it('結果の欠落・早すぎる結果・未対応値を拒否する', () => {
        expect(() => readBackupStatus({...finished, result: null})).toThrow();
        expect(() => readBackupStatus({...running, result: observed})).toThrow();
        expect(() => readBackupStatus({...running, state: 'future'})).toThrow();
        expect(() => readBackupStatus({...running, stage: 'future'})).toThrow();
    });
});

describe('結果と取消', () => {
    it.each(['accepted', 'already_accepted', 'publication_started', 'finished'])('取消の観測を保持する: %s', (decision) => {
        expect(readBackupCancel({executionId: id, decision}, id).decision).toBe(decision);
    });
    it.each(['saved', 'saved_with_warnings', 'not_saved', 'collision', 'unknown'])('結果は保存と再実行可否を別に保持する: %s', (outcome) => {
        expect(readBackupResult({...result, outcome, retryAllowed: false}, id)).toMatchObject({outcome, retryAllowed: false});
    });
    it('成否不明から再実行を許可しない', () => {
        expect(() => readBackupResult({...result, outcome: 'unknown'}, id)).toThrow();
        expect(() => readBackupResult({...result, outcome: 'future'}, id)).toThrow();
        expect(() => readBackupCancel({executionId: id, decision: 'future'}, id)).toThrow();
    });
    it.each([null, [], {executionId: undefined}, {notices: null}, {retryAllowed: 'true'}, {message: ''}, {notices: [{}]}])('欠落・不正応答を成功へ読み替えない: %j', (value) => {
        expect(() => readBackupResult(value && !Array.isArray(value) ? {...result, ...value} : value, id)).toThrow();
    });
    it('返却後の変更で元の通知を書き換えない', () => {
        const parsed = readBackupResult(result, id);
        parsed.notices[0].message = '別の表示';
        expect(result.notices[0].message).toBe('片付けを確認してください。');
    });
});
