(function () {
    const { qs, api, getAuth, ensureWSConnected } = window.GoBlog;

    let page = 1;
    const size = 10;
    let total = 0;
    let onlineRefreshAt = 0;

    function pageEl() { return qs('userPage'); }

    function getTargetUserId() {
        const el = pageEl();
        return (el && el.getAttribute('data-user-id')) || '';
    }

    function setText(id, text) {
        const el = qs(id);
        if (el) el.textContent = text;
    }

    function toNum(v) {
        const n = Number(v);
        return Number.isFinite(n) ? n : 0;
    }

    function renderStats(viewCount, likeCount, commentCount) {
        const parts = [];
        const v = toNum(viewCount);
        const l = toNum(likeCount);
        const c = toNum(commentCount);
        if (v > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">👁</span>' + v + '</span>');
        if (l > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">👍</span>' + l + '</span>');
        if (c > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">💬</span>' + c + '</span>');
        return parts.join(' · ');
    }

    async function loadUser(uid) {
        const resp = await api('/api/v1/users/' + encodeURIComponent(uid));
        const d = resp && resp.data;
        const name = (d && (d.user_name || d.userName)) || '-';
        setText('userId', String((d && (d.user_id || d.userId)) || uid));
        setText('userName', name);
        const title = qs('userTitle');
        if (title) title.textContent = name ? (name + ' 的主页') : '个人主页';
    }

    async function loadUserPoints(uid) {
        try {
            const resp = await api('/api/v1/users/' + encodeURIComponent(uid) + '/points');
            const d = resp && resp.data;
            const bal = toNum(d && (d.balance ?? d.Balance));
            setText('userPoints', String(bal));
        } catch {
            setText('userPoints', '-');
        }
    }

    async function loadOnline(uid) {
        try {
            const resp = await api('/api/v1/users/' + encodeURIComponent(uid) + '/online');
            const d = resp && resp.data;
            const online = !!(d && d.online);
            setText('userOnline', online ? '在线' : '离线');
        } catch {
            setText('userOnline', '-');
        }
    }

    function scheduleOnlineRefresh(uid) {
        const now = Date.now();
        // 避免频繁触发（例如 focus + visibilitychange 连续发生）
        if (now - onlineRefreshAt < 800) return;
        onlineRefreshAt = now;
        loadOnline(uid);
    }

    function setupActions(uid) {
        const followBtn = qs('followUserBtn');
        const dmBtn = qs('dmBtn');
        const msg = qs('userActionMsg');
        if (msg) msg.textContent = '';

        const auth = getAuth();
        const isSelf = auth && auth.userId && String(auth.userId) === String(uid);
        const hasLogin = auth && auth.token;

        if (isSelf) {
            // 访问自己的公开主页时，按需求跳回个人中心
            location.replace('/me');
            return;
        }

        // 私信按钮：先保留入口（最小实现：提示未开放）
        if (dmBtn) {
            dmBtn.style.display = hasLogin ? '' : 'none';
            dmBtn.disabled = !hasLogin;
            dmBtn.onclick = function () {
                if (!hasLogin) return;
                location.href = '/dm?peer_id=' + encodeURIComponent(String(uid));
            };
        }

        // 关注按钮（需登录且非本人）
        if (followBtn) {
            followBtn.style.display = hasLogin ? '' : 'none';
            followBtn.disabled = !hasLogin;
            followBtn.textContent = '关注';
            followBtn.onclick = async function () {
                if (!hasLogin) return;
                if (msg) msg.textContent = '';
                followBtn.disabled = true;
                try {
                    await api('/api/v1/follow', {
                        method: 'POST',
                        body: JSON.stringify({ followingId: String(uid) })
                    });
                    followBtn.textContent = '已关注';
                    if (msg) msg.textContent = '已关注';
                } catch (e) {
                    const m = String(e && e.message || '');
                    // 重复关注也视作已关注
                    if (m.indexOf('重复') !== -1 || m.indexOf('已关注') !== -1 || m.indexOf('double') !== -1) {
                        followBtn.textContent = '已关注';
                        if (msg) msg.textContent = '已关注';
                        return;
                    }
                    followBtn.disabled = false;
                    if (msg) msg.textContent = '关注失败：' + m;
                }
            };
        }
    }

    async function loadArticles(uid) {
        const root = qs('userArticles');
        const empty = qs('userArticlesEmpty');
        const info = qs('userPageInfo');
        const prevBtn = qs('userPrev');
        const nextBtn = qs('userNext');

        root.innerHTML = '';
        if (empty) {
            empty.textContent = '加载中...';
            empty.style.display = '';
        }
        if (info) info.textContent = '';

        try {
            const resp = await api('/api/v1/articles/list?author_id=' + encodeURIComponent(uid) + '&page=' + page + '&size=' + size);
            const data = resp && resp.data;
            const list = (data && data.article_list) || [];
            total = toNum((data && data.total) || 0);

            const maxPage = Math.max(1, Math.ceil((total || 0) / size));
            if (prevBtn) prevBtn.disabled = page <= 1;
            if (nextBtn) nextBtn.disabled = page >= maxPage;
            if (info) info.textContent = '第 ' + page + ' / ' + maxPage + ' 页';

            if (!list.length) {
                if (empty) empty.textContent = '暂无文章';
                return;
            }

            if (empty) empty.style.display = 'none';

            list.forEach((a) => {
                const div = document.createElement('div');
                div.className = 'item';

                const title = document.createElement('div');
                const link = document.createElement('a');
                link.href = '/article/' + encodeURIComponent(a.id);
                link.textContent = a.title || ('文章 ' + a.id);
                title.appendChild(link);

                const summary = document.createElement('div');
                summary.className = 'muted';
                const s = a.summary || a.Summary || '';
                summary.textContent = String(s).slice(0, 160);

                const meta = document.createElement('div');
                meta.className = 'muted';
                meta.innerHTML = renderStats(a.viewCount, a.likeCount, a.commentCount);

                div.appendChild(title);
                if (summary.textContent) div.appendChild(summary);
                if (meta.innerHTML) div.appendChild(meta);

                root.appendChild(div);
            });
        } catch (e) {
            if (empty) {
                empty.textContent = '加载失败：' + (e && e.message || '');
                empty.style.display = '';
            }
        }
    }

    document.addEventListener('DOMContentLoaded', async function () {
        // 登录态全站保持 WS（用于在线状态与通知推送）
        try { ensureWSConnected(); } catch { /* ignore */ }

        const uid = getTargetUserId();
        if (!uid) {
            setText('userTitle', '用户不存在');
            return;
        }

        setupActions(uid);

        // 在线状态：进入页面时拉一次；回到页面/重新聚焦时再拉一次即可
        await loadOnline(uid);
        window.addEventListener('focus', function () { scheduleOnlineRefresh(uid); });
        window.addEventListener('pageshow', function () { scheduleOnlineRefresh(uid); });
        document.addEventListener('visibilitychange', function () {
            if (!document.hidden) scheduleOnlineRefresh(uid);
        });

        // 用户信息 + 文章列表
        try {
            await loadUser(uid);
        } catch {
            // ignore
        }
        await loadUserPoints(uid);

        const prevBtn = qs('userPrev');
        const nextBtn = qs('userNext');
        if (prevBtn) {
            prevBtn.addEventListener('click', async function () {
                if (page <= 1) return;
                page -= 1;
                await loadArticles(uid);
            });
        }
        if (nextBtn) {
            nextBtn.addEventListener('click', async function () {
                const maxPage = Math.max(1, Math.ceil((total || 0) / size));
                if (page >= maxPage) return;
                page += 1;
                await loadArticles(uid);
            });
        }

        await loadArticles(uid);
    });
})();
