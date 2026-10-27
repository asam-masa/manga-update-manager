import {expect, it} from 'vitest';
import {main} from '../wailsjs/go/models';

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
