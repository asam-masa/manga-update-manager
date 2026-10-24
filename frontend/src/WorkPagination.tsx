import type {Ref} from 'react';
import {pageNumbers} from './use-work-pagination';

export function WorkPagination({page, total, count, size, position, disabled, onChange, currentRef}: {
    page: number; total: number; count: number; size: number; position: 'top' | 'bottom'; disabled: boolean;
    onChange: (page: number) => void; currentRef?: Ref<HTMLButtonElement>;
}) {
    if (!count) return null;
    return <nav className="work-pagination" aria-label={position === 'top' ? '一覧上部のページ切替' : '一覧下部のページ切替'}>
        <p className="page-summary" aria-live={position === 'top' ? 'polite' : undefined} aria-atomic="true">
            {page} / {total}ページ・{(page - 1) * size + 1}–{Math.min(page * size, count)}件 / {count}件
        </p>
        <div className="page-buttons">
            <button type="button" className="secondary-button" disabled={disabled || page === 1} onClick={() => onChange(page - 1)}>前へ</button>
            {pageNumbers(page, total).map((n, i) => n === 'gap' ? <span key={`gap-${i}`} aria-hidden="true">…</span> :
                <button key={n} ref={n === page ? currentRef : undefined} type="button" className="secondary-button"
                    aria-label={`${n}ページ`} aria-current={n === page ? 'page' : undefined} disabled={disabled} onClick={() => onChange(n)}>{n}</button>)}
            <button type="button" className="secondary-button" disabled={disabled || page === total} onClick={() => onChange(page + 1)}>次へ</button>
        </div>
    </nav>;
}
