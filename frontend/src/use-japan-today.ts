import {useEffect, useRef, useState} from 'react';

const japanClock = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Tokyo', calendar: 'iso8601', numberingSystem: 'latn',
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
});
const systemNow = () => new Date();
function clockParts(now: Date) {
    const parts = japanClock.formatToParts(now);
    const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((p) => p.type === type)!.value;
    return {day: `${part('year').padStart(4, '0')}-${part('month')}-${part('day')}`,
        hour: Number(part('hour')), minute: Number(part('minute')), second: Number(part('second'))};
}

export const japanToday = (now: Date): string => clockParts(now).day;

// This hook only observes the clock: it never reads/writes data or owns a page.
export function useJapanToday(now: () => Date = systemNow): string {
    const [today, setToday] = useState(() => japanToday(now()));
    const observed = useRef(today);
    useEffect(() => {
        let active = true;
        let midnight: ReturnType<typeof setTimeout>;
        function refresh() {
            if (!active) return;
            const instant = now(), clock = clockParts(instant);
            if (observed.current !== clock.day) {
                observed.current = clock.day;
                setToday(clock.day);
            }
            clearTimeout(midnight);
            // Modern Japan has 24-hour days. Timers may run late; each callback
            // reads the actual clock, and focus/visibility/minute checks recover.
            const delay = ((24 - clock.hour) * 3600 - clock.minute * 60 - clock.second) * 1000 - instant.getUTCMilliseconds();
            midnight = setTimeout(refresh, delay);
        }
        const onVisible = () => { if (document.visibilityState === 'visible') refresh(); };
        refresh();
        const safety = setInterval(refresh, 60_000);
        window.addEventListener('focus', refresh);
        document.addEventListener('visibilitychange', onVisible);
        return () => {
            active = false;
            clearTimeout(midnight); clearInterval(safety);
            window.removeEventListener('focus', refresh);
            document.removeEventListener('visibilitychange', onVisible);
        };
    }, [now]);
    return today;
}
