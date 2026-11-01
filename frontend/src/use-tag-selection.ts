import {useState} from 'react';
import type {LibraryData} from './use-library-snapshot';
import type {Tag} from './tag-edit';

type Selection = {
    data: LibraryData | null;
    enabled: boolean;
    tags: Tag[];
    revision: number;
    notice: {id: number; message: string} | null;
};

// data is an adopted, completely validated result from useLibrarySnapshot.
// Pending/failed/stale reads retain that reference; never pass partial results.
export function useTagSelection(data: LibraryData | null, fresh: boolean, stopped: boolean) {
    const [selection, setSelection] = useState<Selection>({data: null, enabled: false, tags: [], revision: 0, notice: null});
    const enabled = data !== null && fresh && !stopped;
    let current = selection;
    if (selection.data !== data || selection.enabled !== enabled) {
        const available = data && new Map(data.snapshot.tags.map((tag) => [tag.id, tag]));
        const tags = available ? selection.tags.flatMap((tag) => {
            const latest = available.get(tag.id);
            return latest ? [{...latest}] : [];
        }) : selection.tags;
        const removed = tags.length !== selection.tags.length;
        const revision = selection.revision + (removed ? 1 : 0);
        current = {...selection, data, enabled, tags, revision,
            notice: removed ? {id: revision, message: '削除されたタグを条件から外しました。'} : selection.notice};
        // Reconcile before callers filter the new data: no render may pass an
        // unknown selected ID to filterLibrary, or count the old tag as present.
        setSelection(current);
    }

    function select(id: string, checked: boolean) {
        setSelection((previous) => {
            if (!previous.enabled || !previous.data) return previous;
            const tag = previous.data.snapshot.tags.find((value) => value.id === id);
            if (!tag || previous.tags.some((value) => value.id === id) === checked) return previous;
            return {...previous, tags: checked ? [...previous.tags, {...tag}] : previous.tags.filter((value) => value.id !== id),
                revision: previous.revision + 1};
        });
    }
    function clear() {
        setSelection((previous) => previous.enabled && previous.tags.length
            ? {...previous, tags: [], revision: previous.revision + 1} : previous);
    }
    function dismissNotice() {
        setSelection((previous) => previous.notice ? {...previous, notice: null} : previous);
    }

    return {selectedIDs: current.tags.map((tag) => tag.id), selectedTags: current.tags.map((tag) => ({...tag})),
        selectionRevision: current.revision, notice: current.notice && {...current.notice},
        enabled: current.enabled, select, clear, dismissNotice};
}
