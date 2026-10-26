import {StrictMode} from 'react';
import {act, cleanup, fireEvent, renderHook} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {japanToday, useJapanToday} from './use-japan-today';

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); });

it('日本の日付をUTCの日付と区別し、年の下限でも4桁にする', () => {
    expect(japanToday(new Date('2028-02-29T14:59:59.999Z'))).toBe('2028-02-29');
    expect(japanToday(new Date('2028-02-29T15:00:00Z'))).toBe('2028-03-01');
    expect(japanToday(new Date('0001-01-01T12:00:00Z'))).toBe('0001-01-01');
});

it('午前0時のタイマーで更新し、同日の1分ごとの確認では再描画しない', () => {
    vi.setSystemTime(new Date('2028-02-29T14:59:59.750Z'));
    let renders = 0;
    const {result} = renderHook(() => { renders++; return useJapanToday(); });
    expect(result.current).toBe('2028-02-29');
    act(() => { vi.advanceTimersByTime(249); });
    expect(result.current).toBe('2028-02-29');
    act(() => { vi.advanceTimersByTime(1); });
    expect(result.current).toBe('2028-03-01');
    const afterMidnight = renders;
    act(() => { vi.advanceTimersByTime(180_000); });
    expect(renders).toBe(afterMidnight);
});

it('遅れた確認は経過回数を加算せず、注入した時計の前後変更を読む', () => {
    let instant = new Date('2028-02-28T00:00:00Z');
    const now = vi.fn(() => instant);
    const {result} = renderHook(() => useJapanToday(now));
    instant = new Date('2028-03-04T00:00:00Z');
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(result.current).toBe('2028-03-04');
    instant = new Date('2027-12-31T00:00:00Z');
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(result.current).toBe('2027-12-31');
});

it('focusと再表示で確認し、非表示イベントでは変更しない', () => {
    let instant = new Date('2028-02-28T00:00:00Z');
    const now = () => instant;
    const {result} = renderHook(() => useJapanToday(now));
    instant = new Date('2028-03-03T00:00:00Z');
    fireEvent.focus(window);
    expect(result.current).toBe('2028-03-03');
    instant = new Date('2028-03-04T00:00:00Z');
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    fireEvent(document, new Event('visibilitychange'));
    expect(result.current).toBe('2028-03-03');
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    fireEvent(document, new Event('visibilitychange'));
    expect(result.current).toBe('2028-03-04');
});

it('StrictModeや時計の差替えで重複せず、アンマウント後は時計を読まない', () => {
    const first = vi.fn(() => new Date('2028-02-28T00:00:00Z'));
    const second = vi.fn(() => new Date('2028-03-01T00:00:00Z'));
    const {result, rerender, unmount} = renderHook(({now}) => useJapanToday(now), {initialProps: {now: first}, wrapper: StrictMode});
    expect(vi.getTimerCount()).toBe(2);
    rerender({now: second});
    expect(result.current).toBe('2028-03-01');
    expect(vi.getTimerCount()).toBe(2);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
    const calls = second.mock.calls.length;
    fireEvent.focus(window); fireEvent(document, new Event('visibilitychange'));
    act(() => { vi.advanceTimersByTime(120_000); });
    expect(second).toHaveBeenCalledTimes(calls);
});
