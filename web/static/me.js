(function () {
    const { qs, getAuth, api, ensureWSConnected, getWSStatus } = window.GoBlog;

    const DM_HINT_KEY = 'gb_dm_hint';

    let myPage = 1;
    const mySize = 10;
    let myTotal = 0;
    let myLoadSeq = 0;
    let currentGithub = '';

    function normalizeGithubURL(raw) {
        const trimmed = String(raw || '').trim();
        if (!trimmed) return '';

        let candidate = trimmed;
        if (/^@/.test(candidate)) {
            candidate = 'https://github.com/' + candidate.slice(1);
        } else if (!/^https?:\/\//i.test(candidate)) {
            if (/^(www\.)?github\.com\//i.test(candidate)) {
                candidate = 'https://' + candidate;
            } else if (/^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38})$/.test(candidate)) {
                candidate = 'https://github.com/' + candidate;
            } else {
                candidate = 'https://' + candidate;
            }
        }

        try {
            const u = new URL(candidate);
            if (!/^https?:$/i.test(u.protocol)) return '';
            return u.href;
        } catch {
            return '';
        }
    }

    function setDMDot(on) {
        const dot = qs('dmDot');
        if (dot) dot.style.display = on ? 'inline-block' : 'none';
    }

    function setDMHint(on) {
        try {
            if (on) localStorage.setItem(DM_HINT_KEY, '1');
            else localStorage.removeItem(DM_HINT_KEY);
        } catch { /* ignore */ }
        setDMDot(!!on);
    }

    function hasDMHint() {
        try {
            return localStorage.getItem(DM_HINT_KEY) === '1';
        } catch {
            return false;
        }
    }

    async function loadDMUnread() {
        const { token } = getAuth();
        if (!token) return;

        // 先用本地 hint 兜底（避免闪烁）
        const hinted = hasDMHint();
        if (hinted) setDMDot(true);

        try {
            const r = await api('/api/v1/dm/unread_count');
            const d = r && r.data;
            const cnt = Number((d && d.count) || 0) || 0;
            if (cnt > 0) {
                setDMHint(true);
                return;
            }
            // cnt=0：保持本地 hint（用于“在线收到私信但后端未计数”的场景）
            setDMDot(hinted);
        } catch {
            // 请求失败：保持本地 hint
            setDMDot(hinted);
        }
    }

    function isNotificationLike(obj) {
        if (!obj || typeof obj !== 'object') return false;
        // 通知（Kafka payload / MySQL model）通常带这些字段；私信 WS 直推（fromUserId/toUserId）不带。
        return ('event_id' in obj) || ('eventId' in obj) || ('sender_id' in obj) || ('senderId' in obj) || ('sender_name' in obj) || ('senderName' in obj);
    }

    function addNotif(item) {
        const list = qs('notifList');
        const empty = qs('notifEmpty');
        const dot = qs('notifDot');

        if (empty) empty.style.display = 'none';
        if (list) list.style.display = '';
        if (dot) {
            dot.style.display = 'inline-block';
        }

        const li = document.createElement('li');
        const content = item.content || item.Content || '';
        const sender = item.senderName || item.sender_name || '';
        const type = item.type || '';

        const line1 = document.createElement('div');
        line1.textContent = content || JSON.stringify(item);

        const meta = document.createElement('div');
        meta.className = 'muted';
        meta.textContent = (sender ? ('来自 ' + sender + ' · ') : '') + (type ? ('类型 ' + type) : '');

        li.appendChild(line1);
        li.appendChild(meta);
        list.insertBefore(li, list.firstChild);
    }

    function setupWS() {
        const status = qs('wsStatus');
        if (status) status.textContent = getWSStatus ? (getWSStatus() || '未连接') : '未连接';

        window.addEventListener('goblog:ws-status', function (ev) {
            const s = ev && ev.detail && ev.detail.status;
            if (status && s) status.textContent = s;
        });

        window.addEventListener('goblog:ws-message', function (ev) {
            const data = ev && ev.detail && ev.detail.data;
            if (!data) return;

            let obj = null;
            try {
                obj = JSON.parse(data);
            } catch {
                addNotif({ content: String(data) });
                return;
            }

            // 私信相关：点亮“消息”红点（不自动清除，进入 /dm 后清除）
            if (obj && (obj.type === 'dm' || obj.type === 'dm_unread')) {
                setDMHint(true);
                // 兜底刷新一次 Redis 未读计数（避免多端状态不一致）
                loadDMUnread();
            }

            // 只把“通知”类消息展示到通知列表，避免把私信正文混进来
            if (isNotificationLike(obj)) {
                addNotif(obj);
            }
        });

        try { ensureWSConnected(); } catch { /* ignore */ }
    }

    async function loadMeBase() {
        const uidEl = qs('meUserId');
        const unameEl = qs('meUserName');
        const emailEl = qs('meUserEmail');

        const { token, userId, userName } = getAuth();
        // 先用本地缓存兜底显示（避免闪烁）
        if (uidEl) uidEl.textContent = userId || '-';
        if (unameEl) unameEl.textContent = userName || '-';
        if (emailEl) emailEl.textContent = '-';

        if (!token) return;

        try {
            const resp = await api('/api/v1/users/me');
            const d = resp && resp.data;
            const uid = (d && (d.user_id || d.userId)) || userId;
            const uname = (d && (d.user_name || d.userName)) || userName;
            const email = (d && (d.email || d.Email)) || '';
            currentGithub = String((d && (d.github || d.github_url || d.githubUrl)) || '').trim();

            if (uidEl) uidEl.textContent = uid || '-';
            if (unameEl) unameEl.textContent = uname || '-';
            if (emailEl) emailEl.textContent = email || '-';
            const githubInput = qs('meSetGithub');
            if (githubInput) githubInput.value = currentGithub;

            // 同步本地 auth 显示名（便于全站展示）
            const cur = getAuth();
            if (uname && cur && cur.token) {
                window.GoBlog.setAuth({ token: cur.token, userId: String(uid || cur.userId || ''), userName: String(uname) });
            }
        } catch {
            // ignore
        }
    }

    function setupSettings() {
        const btn = qs('meSettingsBtn');
        const panel = qs('meSettingsPanel');
        const cancelBtn = qs('meSettingsCancelBtn');
        const saveBtn = qs('meSettingsSaveBtn');
        const msg = qs('meSettingsMsg');

        function show(on) {
            if (!panel) return;
            panel.style.display = on ? '' : 'none';
            if (!on) {
                const name = qs('meSetName');
                const email = qs('meSetEmail');
                const pwd = qs('meSetPassword');
                const github = qs('meSetGithub');
                if (name) name.value = '';
                if (email) email.value = '';
                if (pwd) pwd.value = '';
                if (github) github.value = currentGithub;
                if (msg) msg.textContent = '';
            } else {
                const github = qs('meSetGithub');
                if (github) github.value = currentGithub;
            }
        }

        if (btn) btn.addEventListener('click', function () { show(true); });
        if (cancelBtn) cancelBtn.addEventListener('click', function () { show(false); });

        if (saveBtn) {
            saveBtn.addEventListener('click', async function () {
                const { token } = getAuth();
                if (!token) {
                    if (msg) msg.textContent = '未登录';
                    return;
                }


                const name = qs('meSetName');
                const email = qs('meSetEmail');
                const pwd = qs('meSetPassword');
                const github = qs('meSetGithub');

                const payload = {};
                if (name && String(name.value || '').trim()) payload.name = String(name.value || '').trim();
                if (email && String(email.value || '').trim()) payload.email = String(email.value || '').trim();
                if (pwd && String(pwd.value || '').trim()) payload.password = String(pwd.value || '').trim();
                if (github) {
                    const rawGithub = String(github.value || '');
                    const trimmedGithub = rawGithub.trim();
                    if (trimmedGithub) {
                        const normalized = normalizeGithubURL(trimmedGithub);
                        if (!normalized) {
                            if (msg) msg.textContent = 'GitHub 链接格式不正确';
                            return;
                        }
                        if (normalized !== currentGithub) {
                            payload.github = normalized;
                        }
                    } else if (currentGithub) {
                        payload.github = '';
                    }
                }

                if (Object.keys(payload).length === 0) {
                    if (msg) msg.textContent = '没有需要更新的内容';
                    return;
                }

                if (msg) msg.textContent = '';
                saveBtn.disabled = true;
                try {
                    await api('/api/v1/users/update', { method: 'POST', body: JSON.stringify(payload) });
                    if (msg) msg.textContent = '已保存';
                    // 更新后刷新一次（优先读 Redis；若缓存被删会回源 DB 并回填）
                    await loadMeBase();
                    show(false);
                } catch (e) {
                    if (msg) msg.textContent = '保存失败：' + (e && e.message || '');
                } finally {
                    saveBtn.disabled = false;
                }
            });
        }
    }

    document.addEventListener('DOMContentLoaded', function () {
        try {
            const { token, userId, userName } = getAuth();
            // 初始化显示 & 从缓存接口读取一次
            loadMeBase();

            setupSettings();

            // 个人中心：消息未读红点
            // 1) 先按本地 hint 显示（避免闪烁）
            setDMDot(hasDMHint());
            // 2) 再按后端 Redis 未读计数修正
            loadDMUnread();

            // 我的文章分页
            const prevBtn = qs('myPrev');
            const nextBtn = qs('myNext');
            if (prevBtn) {
                prevBtn.addEventListener('click', async function () {
                    if (myPage <= 1) return;
                    myPage -= 1;
                    await loadMyArticles(userId);
                });
            }
            if (nextBtn) {
                nextBtn.addEventListener('click', async function () {
                    const maxPage = Math.max(1, Math.ceil((myTotal || 0) / mySize));
                    if (myPage >= maxPage) return;
                    myPage += 1;
                    await loadMyArticles(userId);
                });
            }

            loadMyArticles(userId);

            // 从编辑页/文章页返回时，浏览器可能使用 BFCache 恢复页面，
            // 这时不会重新触发 DOMContentLoaded，导致列表停留在旧状态。
            // 使用 pageshow/focus 在回到页面时刷新一次。
            let lastReloadAt = 0;
            function maybeReload() {
                const now = Date.now();
                if (now - lastReloadAt < 800) return;
                lastReloadAt = now;
                const auth = getAuth();
                loadMyArticles(auth.userId);
                // 回到个人中心时同步一次私信未读红点
                loadDMUnread();
            }
            window.addEventListener('pageshow', function () {
                maybeReload();
            });
            window.addEventListener('focus', function () {
                maybeReload();
            });

            if (!token) {
                const wsEl = qs('wsStatus');
                if (wsEl) wsEl.textContent = '未登录（无法接收通知）';
                return;
            }

            setupWS();
        } catch (e) {
            const empty = qs('myArticlesEmpty');
            if (empty) empty.textContent = '加载失败：脚本错误';
        }
    });

    async function loadMyArticles(userId) {
        const seq = ++myLoadSeq;
        const root = qs('myArticles');
        const empty = qs('myArticlesEmpty');
        const info = qs('myPageInfo');
        const prevBtn = qs('myPrev');
        const nextBtn = qs('myNext');

        root.innerHTML = '';
        empty.textContent = '加载中...';
        empty.style.display = '';
        info.textContent = '';

        if (!userId) {
            empty.textContent = '未登录：无法查看我的文章';
            prevBtn.disabled = true;
            nextBtn.disabled = true;
            return;
        }

        try {
            const resp = await api('/api/v1/articles/list?author_id=' + encodeURIComponent(userId) + '&page=' + myPage + '&size=' + mySize);
            // 如果期间又触发了新的加载请求，丢弃过期响应，避免重复 append
            if (seq !== myLoadSeq) {
                return;
            }
            const data = resp && resp.data;
            const list = (data && data.article_list) || [];
            myTotal = Number((data && data.total) || 0);

            if (!list.length) {
                empty.textContent = '暂无文章';
                prevBtn.disabled = myPage <= 1;
                nextBtn.disabled = true;
                info.textContent = '';
                return;
            }

            empty.style.display = 'none';
            list.forEach((a) => {
                const div = document.createElement('div');
                div.className = 'item row';
                div.style.alignItems = 'flex-start';

                const left = document.createElement('div');
                left.style.flex = '1 1 auto';
                const link = document.createElement('a');
                link.href = '/article/' + encodeURIComponent(a.id);
                link.textContent = a.title || ('文章 ' + a.id);
                left.appendChild(link);

                const summary = document.createElement('div');
                summary.className = 'muted';
                const s = a.summary || a.Summary || '';
                summary.textContent = String(s).slice(0, 160);
                if (summary.textContent) {
                    left.appendChild(summary);
                }

                const meta = document.createElement('div');
                meta.className = 'muted';
                const view = Number(a.viewCount ?? a.view_count ?? 0) || 0;
                const like = Number(a.likeCount ?? a.like_count ?? 0) || 0;
                const comment = Number(a.commentCount ?? a.comment_count ?? 0) || 0;

                const parts = [];
                if (view > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">👁</span>' + view + '</span>');
                if (like > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">👍</span>' + like + '</span>');
                if (comment > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">💬</span>' + comment + '</span>');
                meta.innerHTML = parts.join(' · ');
                left.appendChild(meta);

                const actions = document.createElement('div');
                actions.style.display = 'flex';
                actions.style.flex = '0 0 auto';
                actions.style.gap = '8px';

                const editBtn = document.createElement('button');
                editBtn.type = 'button';
                editBtn.textContent = '编辑';
                editBtn.style.background = '#111';
                editBtn.style.color = '#fff';
                editBtn.style.borderRadius = '8px';
                editBtn.style.padding = '4px 10px';
                editBtn.style.border = '1px solid var(--border)';
                editBtn.onclick = function () {
                    location.href = '/editor?id=' + encodeURIComponent(a.id);
                };

                const delBtn = document.createElement('button');
                delBtn.type = 'button';
                delBtn.textContent = '删除';
                delBtn.style.background = '#111';
                delBtn.style.color = '#fff';
                delBtn.style.borderRadius = '8px';
                delBtn.style.padding = '4px 10px';
                delBtn.style.border = '1px solid var(--border)';
                delBtn.onclick = async function () {
                    if (!confirm('确定删除这篇文章吗？')) return;
                    try {
                        await api('/api/v1/articles/' + encodeURIComponent(a.id), { method: 'DELETE' });
                        await loadMyArticles(userId);
                    } catch (e) {
                        alert('删除失败：' + e.message);
                    }
                };

                actions.appendChild(editBtn);
                actions.appendChild(delBtn);
                div.appendChild(left);
                div.appendChild(actions);
                root.appendChild(div);
            });

            const maxPage = Math.max(1, Math.ceil(myTotal / mySize));
            info.textContent = '第 ' + myPage + ' / ' + maxPage + ' 页，共 ' + myTotal + ' 篇';
            prevBtn.disabled = myPage <= 1;
            nextBtn.disabled = myPage >= maxPage;
        } catch (e) {
            empty.textContent = '加载失败：' + e.message;
            prevBtn.disabled = true;
            nextBtn.disabled = true;
        }
    }
})();
