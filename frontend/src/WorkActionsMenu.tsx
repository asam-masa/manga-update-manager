import {useLayoutEffect, useRef, useState} from 'react';
import type {ReactNode} from 'react';

// A disclosure containing ordinary buttons: Tab keeps its native document order.
export function WorkActionsMenu({identity, title, open, onChange, children}: {
    identity: string; title: string; open: boolean; onChange: (open: boolean) => void;
    children: (run: (action: (origin: HTMLButtonElement) => void) => void) => ReactNode;
}) {
    const root = useRef<HTMLDivElement>(null);
    const trigger = useRef<HTMLButtonElement>(null);
    const panel = useRef<HTMLDivElement>(null);
    const close = useRef(onChange);
    close.current = onChange;
    const last = useRef(false);
    const [position, setPosition] = useState({left: 8, top: 8});
    const buttons = () => Array.from(panel.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? []);
    useLayoutEffect(() => {
        if (!open || !trigger.current || !panel.current) return;
        const anchor = trigger.current.getBoundingClientRect();
        const bounds = panel.current.getBoundingClientRect();
        setPosition({left: Math.max(8, Math.min(anchor.right - bounds.width, innerWidth - bounds.width - 8)),
            top: anchor.bottom + bounds.height + 4 <= innerHeight - 8 ? anchor.bottom + 4 : Math.max(8, anchor.top - bounds.height - 4)});
        const items = buttons();
        (last.current ? items.at(-1) : items[0])?.focus();
    }, [open]);
    useLayoutEffect(() => {
        if (!open) return;
        const outside = (event: Event) => {
            if (event.target instanceof Node && !root.current?.contains(event.target)) close.current(false);
        };
        const moved = () => close.current(false);
        const scrolled = (event: Event) => {
            // A constrained panel can scroll internally without closing itself.
            if (!(event.target instanceof Node) || !panel.current?.contains(event.target)) moved();
        };
        document.addEventListener('pointerdown', outside, true);
        document.addEventListener('focusin', outside);
        window.addEventListener('scroll', scrolled, true);
        window.addEventListener('resize', moved);
        return () => {
            document.removeEventListener('pointerdown', outside, true);
            document.removeEventListener('focusin', outside);
            window.removeEventListener('scroll', scrolled, true);
            window.removeEventListener('resize', moved);
        };
    }, [open]);
    function show(fromEnd: boolean) {
        last.current = fromEnd;
        if (open) { const items = buttons(); (fromEnd ? items.at(-1) : items[0])?.focus(); }
        else onChange(true);
    }
    function run(action: (origin: HTMLButtonElement) => void) {
        onChange(false);
        if (trigger.current) { trigger.current.focus(); action(trigger.current); }
    }
    return <div ref={root} className="work-actions" onKeyDown={(event) => {
        if (!open) return;
        if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onChange(false); trigger.current?.focus(); }
        else if (panel.current?.contains(event.target as Node) && ['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
            event.preventDefault();
            const items = buttons();
            const index = items.indexOf(document.activeElement as HTMLButtonElement);
            const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1
                : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
            items[next]?.focus();
        }
    }}>
        <button ref={trigger} id={`work-actions-${identity}`} type="button" className="secondary-button work-actions-trigger"
            aria-label={`${title}の作品操作`} aria-expanded={open} aria-controls={`work-panel-${identity}`}
            onClick={() => { if (open) onChange(false); else show(false); }} onKeyDown={(event) => {
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); show(event.key === 'ArrowUp'); }
            }}>⋯</button>
        {open && <div ref={panel} id={`work-panel-${identity}`} role="group" aria-label={`${title}の作品操作一覧`}
            className="work-actions-panel" style={position}>{children(run)}</div>}
    </div>;
}
