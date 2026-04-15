(function () {
    function qs(id) { return document.getElementById(id); }

    function getAuth() {
        return {
            token: localStorage.getItem('gb_token') || '',
            userId: localStorage.getItem('gb_user_id') || '',
            userName: localStorage.getItem('gb_user_name') || ''
        };
    }

    function setAuth(auth) {
        localStorage.setItem('gb_token', auth.token || '');
        localStorage.setItem('gb_user_id', auth.userId || '');
        localStorage.setItem('gb_user_name', auth.userName || '');
    }

    function clearAuth() {
        localStorage.removeItem('gb_token');
        localStorage.removeItem('gb_user_id');
        localStorage.removeItem('gb_user_name');
    }

    function authHeader() {
        const { token } = getAuth();
        return token ? { 'Authorization': 'Bearer ' + token } : {};
    }

    async function api(path, opts) {
        const res = await fetch(path, Object.assign({
            cache: 'no-store',
            headers: Object.assign({ 'Content-Type': 'application/json' }, authHeader(), (opts && opts.headers) || {})
        }, opts || {}));

        const isJson = (res.headers.get('content-type') || '').includes('application/json');
        const body = isJson ? await res.json() : await res.text();

        if (!res.ok) {
            const msg = (body && body.message) || (body && body.error) || ('HTTP ' + res.status);
            throw new Error(msg);
        }

        if (body && typeof body === 'object' && 'code' in body && body.code !== 0) {
            throw new Error(body.message || '请求失败');
        }

        return body;
    }

    function setupNav() {
        const { token } = getAuth();
        const navLogin = qs('navLogin');
        const navLogout = qs('navLogout');
        const navHome = qs('navHome');
        const navMe = qs('navMe');
        // 登录/退出按钮 & 个人中心 / 登录链接显示控制
        if (navLogin) {
            navLogin.style.display = token ? 'none' : '';
        }
        if (navMe) {
            navMe.style.display = token ? '' : 'none';
        }
        if (navLogout) {
            navLogout.style.display = token ? '' : 'none';
            if (token) {
                navLogout.onclick = function () {
                    clearAuth();
                    location.href = '/login';
                };
            } else {
                navLogout.onclick = null;
            }
        }

        // 顶部导航激活态
        const path = location.pathname || '/';

        if (navHome) {
            navHome.classList.toggle('nav-active', path === '/');
        }
        if (navMe) {
            navMe.classList.toggle('nav-active', path === '/me');
        }
    }

    window.GoBlog = {
        qs,
        api,
        getAuth,
        setAuth,
        clearAuth,
        setupNav,
    };

    document.addEventListener('DOMContentLoaded', setupNav);
})();
