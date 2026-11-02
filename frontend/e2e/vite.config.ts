import {defineConfig, mergeConfig} from 'vite';
import productConfig from '../vite.config.ts';

// Build the product entry unchanged. Fail even for a tree-shaken test import.
export default mergeConfig(productConfig, defineConfig({
    plugins: [{
        name: 'reject-browser-test-modules',
        moduleParsed({id}) {
            const path = id.replaceAll('\\', '/');
            if (path.includes('/e2e/') || /\/(?:@playwright|playwright|playwright-core)\//.test(path)) {
                throw new Error(`Browser test module entered product build: ${id}`);
            }
        },
    }],
}));
