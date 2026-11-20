import {useCallback, useEffect, useState} from 'react';
import {GetBackupOmittedImages, GetBackupOmittedImageWorks} from '../wailsjs/go/main/App';
import {readOmissionPage, validateOmissionRequest} from './backup-omission-page';
import type {OmissionPage, OmissionRequest} from './backup-omission-page';

type DetailAPI = {images: (id: string, page: number) => Promise<unknown>; works: (id: string, image: string, page: number) => Promise<unknown>};
const detailAPI: DetailAPI = {images: GetBackupOmittedImages, works: GetBackupOmittedImageWorks};
export function useBackupOmissionPage(request: OmissionRequest | null, api: DetailAPI = detailAPI) {
    const id = request?.executionId, image = request?.imageId, page = request?.page;
    const key = request === null ? '' : JSON.stringify([id, image === undefined ? 'images' : 'works', image, page]);
    const [revision, setRevision] = useState(0);
    const [state, setState] = useState<{key: string; data: OmissionPage | null; loading: boolean; error: string}>(
        {key: '', data: null, loading: false, error: ''});
    const reload = useCallback(() => setRevision((old) => old + 1), []);
    useEffect(() => {
        let active = true;
        if (!key) {setState({key, data: null, loading: false, error: ''}); return;}
        const selected: OmissionRequest = {executionId: id!, page: page!, imageId: image};
        setState({key, data: null, loading: true, error: ''});
        void (async () => {
            try {
                validateOmissionRequest(selected);
                const value = selected.imageId === undefined ? await api.images(selected.executionId, selected.page)
                    : await api.works(selected.executionId, selected.imageId, selected.page);
                if (active) setState({key, data: readOmissionPage(value, selected), loading: false, error: ''});
            } catch {
                if (active) setState({key, data: null, loading: false,
                    error: '除外画像・対象作品の詳細を取得できません。詳細を再読み込みしてください。'});
            }
        })();
        return () => {active = false;};
    }, [id, image, page, key, revision, api.images, api.works]);
    return {...(state.key === key ? state : {key, data: null, loading: request !== null, error: ''}), reload};
}
