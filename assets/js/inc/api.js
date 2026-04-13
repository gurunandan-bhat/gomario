/**
 * Thin wrapper around fetch for JSON API calls.
 * Automatically fetches and attaches the CSRF token on state-mutating requests.
 */

let _csrfToken = null;

async function csrfToken() {
    if (!_csrfToken) {
        const res = await fetch('/api/csrf-token');
        const data = await res.json();
        _csrfToken = data.csrfToken;
    }
    return _csrfToken;
}

/**
 * @param {string} path
 * @param {RequestInit} options
 */
export async function apiGet(path, options = {}) {
    const res = await fetch(path, {
        ...options,
        headers: { 'Content-Type': 'application/json', ...options.headers },
    });
    if (!res.ok) throw new Error((await res.json()).message ?? res.statusText);
    return res.json();
}

/**
 * @param {string} path
 * @param {unknown} body
 * @param {RequestInit} options
 */
export async function apiPost(path, body, options = {}) {
    const token = await csrfToken();
    const res = await fetch(path, {
        method: 'POST',
        ...options,
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': token,
            ...options.headers,
        },
        body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error((await res.json()).message ?? res.statusText);
    return res.json();
}
