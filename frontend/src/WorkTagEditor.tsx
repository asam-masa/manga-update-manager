import {useRef, useState} from 'react';
import {titleMatches} from './work-search';
import {tagName, validTagName} from './tag-edit';
import type {useWorkTagEdit} from './use-work-tag-edit';

export function WorkTagEditor({tags, disabled}: {tags: ReturnType<typeof useWorkTagEdit>; disabled: boolean}) {
    const [query, setQuery] = useState(''), [error, setError] = useState('');
    const search = useRef<HTMLInputElement>(null);
    const locked = disabled || tags.loading || !!tags.error;
    const name = tagName(query);
    function add() {
        if (locked) return;
        if (!validTagName(name)) {setError('タグ名は前後の空白を除いて1〜50文字で、改行・制御文字を含めず入力してください。'); return;}
        const existing = tags.candidates.find((tag) => tag.name === name);
        if (existing) {
            if (!tags.existing.some((tag) => tag.id === existing.id)) tags.update([...tags.existing, existing]);
        } else if (!tags.newNames.includes(name)) tags.update(tags.existing, [...tags.newNames, name]);
        setQuery(''); setError(''); search.current?.focus();
    }
    function remove(id: string) { tags.update(tags.existing.filter((tag) => tag.id !== id)); search.current?.focus(); }
    return <section className="work-tag-editor" aria-labelledby="edit-tags-heading" aria-busy={tags.loading}>
        <h3 id="edit-tags-heading">タグ <span className="optional">任意</span></h3>
        <div className="tag-box">
            {tags.loading && <p role="status">タグを読み込んでいます。入力は続けられます。</p>}
            {tags.error && <div className="message error-message" role="alert"><p>{tags.error}</p>
                <button type="button" className="secondary-button" disabled={disabled || tags.loading} onClick={() => { void tags.reload(); }}>タグを再読み込み</button></div>}
            {!tags.loading && !tags.error && tags.conflicts.length > 0 && <div className="tag-conflicts" role="status">
                <p>選択したタグが変更されました。入力は残っています。変更を確認してください。</p>
                {tags.conflicts.map((old) => {
                    const latest = tags.candidates.find((tag) => tag.id === old.id);
                    return <div key={old.id} className="tag-conflict">
                        <p>{latest ? `改名：「${old.name}」→「${latest.name}」` : `削除：「${old.name}」`}</p>
                        {latest ? <button type="button" className="secondary-button" disabled={disabled} onClick={() => {
                            tags.update(tags.existing.map((tag) => tag.id === old.id ? latest : tag)); search.current?.focus();
                        }}>「{latest.name}」を採用する</button>
                            : <button type="button" className="secondary-button" disabled={disabled} onClick={() => remove(old.id)}>「{old.name}」を選択から外す</button>}
                    </div>;
                })}
            </div>}
            <div className="tag-chips" aria-label="選択したタグ">
                {tags.existing.map((tag) => <span className="tag-chip" key={tag.id}><span>{tag.name}</span>
                    <button type="button" disabled={locked} aria-label={`タグ「${tag.name}」を外す`} onClick={() => remove(tag.id)}>×</button></span>)}
                {tags.newNames.map((value) => <span className="tag-chip tag-new" key={value}><span>{value}</span><span className="tag-new-label">新規</span>
                    <button type="button" disabled={locked} aria-label={`新規タグ「${value}」を外す`} onClick={() => {tags.update(tags.existing, tags.newNames.filter((n) => n !== value)); search.current?.focus();}}>×</button></span>)}
                {tags.initialized && !tags.existing.length && !tags.newNames.length && <p className="section-description">選択したタグはありません。</p>}
            </div>
            <label htmlFor="edit-tag-query">候補を検索／新しいタグ名を入力</label>
            <input ref={search} id="edit-tag-query" value={query} disabled={locked} autoComplete="off" onChange={(event) => {setQuery(event.target.value); setError('');}}
                onKeyDown={(event) => {if (event.key === 'Enter') event.preventDefault();}} aria-describedby="edit-tag-help" />
            {!tags.loading && !tags.error && <div className="tag-candidates" aria-label="タグ候補">
                {tags.candidates.filter((tag) => titleMatches(tag.name, query)).map((tag) => <label key={tag.id}>
                    <input type="checkbox" checked={tags.existing.some((selected) => selected.id === tag.id)}
                        disabled={disabled || tags.conflicts.some((old) => old.id === tag.id)} onChange={(event) => {
                            tags.update(event.target.checked ? [...tags.existing, tag] : tags.existing.filter((selected) => selected.id !== tag.id));
                        }} />{tag.name}</label>)}
                {!tags.candidates.some((tag) => titleMatches(tag.name, query)) && <p className="section-description">一致する候補はありません。</p>}
            </div>}
            {name && !tags.candidates.some((tag) => tag.name === name) && !tags.newNames.includes(name) && <button type="button" className="secondary-button tag-create" disabled={locked} onClick={add}>＋「{name}」を新規タグとして仮追加</button>}
            {error && <p className="message error-message" role="alert">{error}</p>}
            <p id="edit-tag-help" className="section-description">新規タグは仮追加です。作品の保存時にまとめて確定します。</p>
        </div>
    </section>;
}
