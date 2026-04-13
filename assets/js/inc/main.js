import '@tabler/core';
import { flash } from './utils.js';
import { apiGet, apiPost } from './api.js';

// Expose helpers globally so inline scripts in templates can reach them.
window.App = { flash, apiGet, apiPost };
