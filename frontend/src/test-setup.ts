import {afterEach, vi} from 'vitest';
import {cleanup} from '@testing-library/react';

afterEach(cleanup);
vi.mock('../wailsjs/runtime/runtime', () => ({EventsOn: vi.fn(() => () => {})}));
