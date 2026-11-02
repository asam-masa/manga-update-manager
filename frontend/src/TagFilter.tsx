import {useEffect, useLayoutEffect, useRef, useState} from 'react';
import type {RefObject} from 'react';
import type {Tag} from './tag-edit';
import type {TagCandidate} from './tag-filter';

export function TagFilterPopup({open, setOpen, available, enabled, candidates, totalTags, selectedIDs, trigger,
    onQuery, onSelect}: {open: boolean; setOpen: (open: boolean) => void; available: boolean; enabled: boolean;
    candidates: TagCandidate[]; totalTags: number; selectedIDs: string[]; trigger: RefObject<HTMLButtonElement | null>;
    onQuery: (query: string) => void; onSelect: (id: string, checked: boolean) => void}) {
    const anchor = useRef<HTMLDivElement>(null), search = useRef<HTMLInputElement>(null);
    const composing = useRef(false);
    const [draft, setDraft] = useState('');
    useLayoutEffect(() => {if (open) search.current?.focus();}, [open]);
    useEffect(() => {
        if (!open) return;
        const pointer = (event: PointerEvent) => {
            if (event.target instanceof Node && !anchor.current?.contains(event.target)) setOpen(false);
        };
        const key = (event: KeyboardEvent) => {
            if (event.key !== 'Escape' || event.isComposing) return;
            event.preventDefault(); setOpen(false); trigger.current?.focus();
        };
        document.addEventListener('pointerdown', pointer);
        document.addEventListener('keydown', key);
        return () => {document.removeEventListener('pointerdown', pointer); document.removeEventListener('keydown', key);};
    }, [open, setOpen, trigger]);
    return <div className="tag-filter-anchor" ref={anchor}>
        <button ref={trigger} type="button" className="secondary-button" disabled={!available}
            aria-expanded={open} aria-controls="tag-filter-popup" onClick={() => setOpen(!open)}>
            タグで絞り込み{selectedIDs.length > 0 && `（${selectedIDs.length}）`}</button>
        {open && <section id="tag-filter-popup" className="tag-filter-popup" aria-label="タグ絞り込みの候補">
            <div className="tag-filter-heading"><strong>タグで絞り込み</strong>
                <button type="button" className="secondary-button" aria-label="タグ候補を閉じる"
                    onClick={() => {setOpen(false); trigger.current?.focus();}}>閉じる</button></div>
            <label htmlFor="tag-filter-search">候補のタグ名で検索</label>
            <input ref={search} id="tag-filter-search" type="search" value={draft} placeholder="タグ名で候補を検索"
                onCompositionStart={() => {composing.current = true;}}
                onCompositionEnd={(event) => {composing.current = false; setDraft(event.currentTarget.value); onQuery(event.currentTarget.value);}}
                onChange={(event) => {setDraft(event.target.value); if (!composing.current) onQuery(event.target.value);}} />
            <p className="section-description">現在の条件にそのタグも加えた場合の作品数です。</p>
            {!enabled && <p role="status">最新情報は未確認です。タグ条件は変更できません。</p>}
            <div className="tag-filter-candidates">
                {candidates.map(({tag, workCount}) => <label key={tag.id}>
                    <input type="checkbox" checked={selectedIDs.includes(tag.id)} disabled={!enabled}
                        onChange={(event) => onSelect(tag.id, event.target.checked)} />
                    <span>{tag.name}</span><span className="tag-filter-count">{workCount}件{!enabled && '（未確認）'}</span>
                </label>)}
                {candidates.length === 0 && <p>{totalTags === 0 ? 'タグはまだありません。作品の編集で追加できます。' : '一致するタグ候補がありません。'}</p>}
            </div>
        </section>}
    </div>;
}

export function SelectedTagFilters({tags, enabled, trigger, onRemove, onClear}: {tags: Tag[]; enabled: boolean;
    trigger: RefObject<HTMLButtonElement | null>; onRemove: (id: string) => void; onClear: () => void}) {
    const pending = useRef<string | null>(null);
    const key = tags.map((tag) => tag.id).join(',');
    useLayoutEffect(() => {
        if (pending.current === null) return;
        const id = pending.current; pending.current = null;
        if (document.activeElement !== document.body) return;
        const next = document.getElementById(`tag-condition-${id}`);
        if (next instanceof HTMLButtonElement && !next.disabled) next.focus(); else trigger.current?.focus();
    }, [key, trigger]);
    if (!tags.length) return null;
    return <div className="selected-tag-filters" role="group" aria-label="選択中のタグ条件">
        {tags.map((tag, index) => <span className="tag-chip" key={tag.id}><span>{tag.name}</span>
            <button id={`tag-condition-${tag.id}`} type="button" aria-label={`「${tag.name}」の条件を解除`} disabled={!enabled}
                onClick={() => {pending.current = tags[index + 1]?.id ?? tags[index - 1]?.id ?? ''; onRemove(tag.id);}}>×</button></span>)}
        <button type="button" className="secondary-button" disabled={!enabled}
            onClick={() => {pending.current = ''; onClear();}}>すべて解除</button>
    </div>;
}
