import {expect, it} from 'vitest';
import {main} from '../wailsjs/go/models';

it('管理用の生成型でIDと登録日時の文字列を丸めず往復する', () => {
    const source = {tag: {id: '9223372036854775807', name: 'タグ'}, works: [{id: '9007199254740993', createdAt: '2026-10-28T10:02:03.123456789+09:00', title: '作品'}]};
    const preview = main.TagDeletePreviewDTO.createFrom(source);
    expect(JSON.parse(JSON.stringify(preview))).toEqual(source);
    const usage = {tag: source.tag, workCount: 2};
    expect(JSON.parse(JSON.stringify(main.TagUsageDTO.createFrom(usage)))).toEqual(usage);
});

it('生成モデルを通した編集・削除入力でも既存のタグ省略を保持する', () => {
    for (const input of [main.EditWorkInput.createFrom({url: 'https://example.com', title: '作品'}), main.DeleteWorkInput.createFrom({url: 'https://example.com', title: '作品'})]) {
        expect(JSON.parse(JSON.stringify(input))).not.toHaveProperty('tags');
    }
});

it('生成モデルとJSONの往復でタグIDの全桁と新規入力を保持する', () => {
    const tags = [{id: '9223372036854775807', name: 'タグ'}];
    const edit = main.EditWorkInput.createFrom({tags: {existing: tags, newNames: [' 新規 ']}});
    expect(JSON.parse(JSON.stringify(edit)).tags).toEqual({existing: tags, newNames: [' 新規 ']});
    const deletion = main.DeleteWorkInput.createFrom({tags});
    expect(JSON.parse(JSON.stringify(deletion)).tags).toEqual(tags);
});
