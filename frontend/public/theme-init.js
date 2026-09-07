// Apply the stored theme preference before first paint to avoid a flash of
// the wrong theme. Loaded synchronously from index.html (a file rather than
// an inline script, so the content security policy can allow scripts from
// the app's own origin only). Keep the storage key in sync with
// src/theme.ts (THEME_STORAGE_KEY).
(function () {
    try {
        var t = localStorage.getItem('openv-theme');
        if (t === 'light' || t === 'dark') {
            document.documentElement.setAttribute('data-theme', t);
        }
    } catch (e) { /* localStorage unavailable — system theme applies */ }
})();
