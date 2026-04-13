/**
 * Show a dismissible flash message at the top of the page.
 * @param {string} message
 * @param {'success'|'danger'|'warning'|'info'} type
 */
export function flash(message, type = 'info') {
    const el = document.createElement('div');
    el.className = `alert alert-${type} alert-dismissible fade show`;
    el.setAttribute('role', 'alert');
    el.innerHTML = `${message}<button type="button" class="btn-close" data-bs-dismiss="alert"></button>`;
    document.body.prepend(el);
}

/**
 * Read a cookie value by name. Used to retrieve the CSRF token for fetch calls.
 * @param {string} name
 * @returns {string}
 */
export function getCookie(name) {
    const match = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'));
    return match ? decodeURIComponent(match[1]) : '';
}
