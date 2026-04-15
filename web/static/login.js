(function () {
    const { qs, api, setAuth } = window.GoBlog;

    async function onSubmit(ev) {
        ev.preventDefault();

        const form = ev.target;
        const msg = qs('loginMsg');
        msg.textContent = '';

        const tel = String(form.tel.value || '').trim();
        const password = String(form.password.value || '');
        if (!tel || !password) {
            msg.textContent = '请填写手机号和密码';
            return;
        }

        try {
            const resp = await api('/api/v1/login', {
                method: 'POST',
                body: JSON.stringify({ tel, password })
            });

            const data = resp && resp.data;
            if (!data || !data.token) {
                throw new Error('登录返回缺少 token');
            }

            setAuth({
                token: data.token,
                userId: String(data.user_id || ''),
                userName: String(data.user_name || '')
            });

            location.href = '/';
        } catch (e) {
            msg.textContent = '登录失败：' + e.message;
        }
    }

    document.addEventListener('DOMContentLoaded', function () {
        const form = qs('loginForm');
        form.addEventListener('submit', onSubmit);

        const regForm = qs('registerForm');
        regForm.addEventListener('submit', async function (ev) {
            ev.preventDefault();

            const msg = qs('registerMsg');
            msg.textContent = '';

            const username = String(regForm.username.value || '').trim();
            const tel = String(regForm.tel.value || '').trim();
            const password = String(regForm.password.value || '');
            if (!username || !tel || !password) {
                msg.textContent = '请填写用户名、手机号和密码';
                return;
            }

            try {
                const signupResp = await api('/api/v1/signup', {
                    method: 'POST',
                    body: JSON.stringify({ username, tel, password })
                });

                const userId = signupResp && signupResp.data && signupResp.data.user_id;
                if (!userId) {
                    throw new Error('注册返回缺少 user_id');
                }

                const loginResp = await api('/api/v1/login', {
                    method: 'POST',
                    body: JSON.stringify({ tel, password })
                });
                const data = loginResp && loginResp.data;
                if (!data || !data.token) {
                    throw new Error('自动登录失败：缺少 token');
                }

                setAuth({
                    token: data.token,
                    userId: String(data.user_id || ''),
                    userName: String(data.user_name || '')
                });

                location.href = '/';
            } catch (e) {
                msg.textContent = '注册失败：' + e.message;
            }
        });

        // 登录 / 注册切换
        const loginPane = qs('loginPane');
        const registerPane = qs('registerPane');
        const toRegister = qs('toRegister');
        const toLogin = qs('toLogin');
        const title = qs('authTitle');
        const desc = qs('authDesc');

        if (toRegister && toLogin && loginPane && registerPane && title && desc) {
            toRegister.addEventListener('click', function () {
                loginPane.style.display = 'none';
                registerPane.style.display = '';
                title.textContent = '注册';
                desc.textContent = '注册成功后会自动登录，并跳转到首页。';
            });

            toLogin.addEventListener('click', function () {
                registerPane.style.display = 'none';
                loginPane.style.display = '';
                title.textContent = '登录';
                desc.textContent = '使用手机号 + 密码登录。';
            });
        }
    });
})();
