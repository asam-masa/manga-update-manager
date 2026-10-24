// No browser, dependencies, product API or user data: check the mock's rendering logic.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
const source = html.match(/<script>([\s\S]*?)<\/script>/)[1];

function boot(search = '') {
    const elements = {app: {}, results: {addEventListener() {}}, '.count': {}, input: {addEventListener() {}}};
    const context = vm.createContext({
        URLSearchParams, location: {search},
        document: {getElementById: key => elements[key], querySelector: key => elements[key], querySelectorAll: () => []},
    });
    vm.runInContext(source, context);
    return {context, elements};
}

for (const [mode, size] of [['compact', 24], ['rows', 20], ['cards', 12]]) {
    const {context, elements} = boot('?mode=' + mode);
    const content = () => elements.results.innerHTML;
    const count = () => (content().match(mode === 'rows' ? /<tr><td>/g : /<li class="entry">/g) || []).length;
    assert.equal(count(), size, mode + ' first page');
    assert.equal((content().match(/class="pager"/g) || []).length, 2);
    assert.ok(content().includes('1–' + size + '件 / 60件'));
    vm.runInContext('page=2;render()', context);
    assert.equal(count(), size);
    assert.ok(content().includes((size + 1) + '–' + (size * 2) + '件 / 60件'));
    vm.runInContext('page=99;render()', context);
    assert.equal(vm.runInContext('page', context), Math.ceil(60 / size));
    assert.equal(count(), 60 % size || size);
    vm.runInContext("query='not-found';page=99;render()", context);
    assert.ok(content().includes('0–0件 / 0件'));
    assert.equal(count(), 0);
    assert.equal(vm.runInContext('page', context), 1);
}

for (const columns of [4, 5, 6]) {
    const {context, elements} = boot('?mode=compact&columns=' + columns);
    assert.equal(vm.runInContext('compactColumns', context), columns);
    assert.ok(elements.results.innerHTML.includes('--compact-columns:' + columns));
}
assert.equal(vm.runInContext('mode', boot('?mode=invalid').context), 'cards');
assert.equal(vm.runInContext('compactColumns', boot('?columns=invalid').context), 5);
assert.ok(html.includes('.compact .title{-webkit-line-clamp:3;height:72px}'));
assert.ok(html.includes('grid-row:3;grid-column:1'));
console.log('Mock checks passed: 3 modes, trial page sizes/bounds/empty results, fixed columns and safe defaults.');
