import {useEffect, useRef, useState} from 'react';
import {LoadCoverProgress} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';
import type {WorkView} from './work-display';

export type CoverProgressTarget = {operationID: string; workID: number; kind: 'select' | 'remove' | 'cleanup'};
type Observation = {sample?: main.CoverProgressDTO; unavailable: boolean};
const stages: Record<string, string> = {checking: '情報を確認', choosing: '画像を選択', generating: '画像を読取・生成',
    preparing: '保存領域を準備', saving: '画像を保存', waiting: '参照更新の順番待ち', updating: '保存情報を更新',
    cleaning: '不要画像の後処理', releasing: '画像・フォルダーを解放'};
const key = (target: Pick<main.CoverProgressDTO, 'kind' | 'workID' | 'operationID'>) => `${target.kind}:${target.workID}:${target.operationID}`;
function valid(value: unknown): value is main.CoverProgressDTO {
    if (typeof value !== 'object' || value === null) return false;
    const sample = value as main.CoverProgressDTO;
    return typeof sample.operationID === 'string' && Number.isSafeInteger(sample.workID)
        && ['select', 'remove', 'cleanup'].includes(sample.kind) && typeof sample.stage === 'string' && Object.hasOwn(stages, sample.stage)
        && typeof sample.timed === 'boolean' && Number.isSafeInteger(sample.elapsedMilliseconds)
        && sample.elapsedMilliseconds >= 0 && (sample.timed || sample.elapsedMilliseconds === 0);
}

// App owns targets; filtering cards never changes this component's lifetime.
export function CoverProgressNotice({targets, works}: {targets: CoverProgressTarget[]; works: WorkView[] | null}) {
    const currentTargets = useRef(targets);
    currentTargets.current = targets;
    const inFlight = useRef(false);
    const generation = useRef(0);
    const [observations, setObservations] = useState<Record<string, Observation>>({});
    const active = targets.length > 0;
    useEffect(() => { if (!active) setObservations({}); }, [active]);
    useEffect(() => {
        const current = ++generation.current;
        if (!active) return () => { ++generation.current; };
        const timer = setInterval(() => {
            if (inFlight.current || currentTargets.current.length === 0) return;
            const requested = new Set(currentTargets.current.map(key));
            inFlight.current = true;
            void (async () => {
                let samples: main.CoverProgressDTO[] | undefined;
                try {
                    const result: unknown = await LoadCoverProgress();
                    if (!Array.isArray(result) || !result.every(valid)) throw new Error('Invalid progress');
                    const keys = result.map(key);
                    if (new Set(keys).size !== keys.length) throw new Error('Duplicate progress');
                    samples = result;
                } catch { /* Only a fixed safe notice is rendered below. */ }
                finally { inFlight.current = false; }
                if (current !== generation.current) return;
                setObservations((previous) => {
                    const next: Record<string, Observation> = {};
                    for (const target of currentTargets.current) {
                        const id = key(target);
                        if (!requested.has(id)) { if (previous[id]) next[id] = previous[id]; continue; }
                        const sample = samples?.find((item) => key(item) === id);
                        next[id] = sample ? {sample, unavailable: false}
                            : {sample: previous[id]?.sample, unavailable: samples === undefined || previous[id]?.sample !== undefined};
                    }
                    return next;
                });
            })();
        }, 1000);
        return () => { ++generation.current; clearInterval(timer); };
    }, [active]);
    const rows = targets.flatMap((target) => {
        const observation = observations[key(target)];
        return observation?.sample?.timed && observation.sample.elapsedMilliseconds >= 10000
            ? [{target, ...observation, sample: observation.sample}] : [];
    });
    const unavailable = targets.some((target) => observations[key(target)]?.unavailable);
    if (rows.length === 0 && !unavailable) return null;
    return <section className="message progress-message" role="status" aria-label="画像処理の状況">
        <h2>画像処理の状況</h2>
        <p>画像処理の完了を待っています。処理中は終了できません。完了後にもう一度閉じてください。</p>
        {unavailable && <p>現在の処理状況を確認できません。操作の結果をお待ちください。表示がある場合は前回確認した情報です。</p>}
        <ul>{rows.map(({target, sample}) => <li key={key(target)}>
            {target.kind === 'cleanup' ? '起動時の画像整理' : works?.find((work) => work.id === target.workID)?.title ?? `作品ID ${target.workID}`}：
            {stages[sample.stage]}（最終確認時の経過 {Math.floor(sample.elapsedMilliseconds / 1000)}秒）
        </li>)}</ul>
        <p>処理段階は最後に確認した状態です。正常動作やフリーズの有無を保証するものではありません。</p>
    </section>;
}
