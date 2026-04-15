(function () {
    const { qs, getAuth, api } = window.GoBlog;

    let myPage = 1;
    const mySize = 10;
    let myTotal = 0;
    let myLoadSeq = 0;

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

    function connectWS(token) {
        const status = qs('wsStatus');

        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = proto + '//' + location.host + '/api/v1/ws?token=' + encodeURIComponent(token);

        status.textContent = '连接中...';
        const ws = new WebSocket(url);

        ws.onopen = function () {
            status.textContent = '已连接';
        };
        ws.onclose = function () {
            status.textContent = '已断开';
        };
        ws.onerror = function () {
            status.textContent = '连接错误';
        };
        ws.onmessage = function (ev) {
            try {
                const obj = JSON.parse(ev.data);
                addNotif(obj);
            } catch {
                addNotif({ content: String(ev.data) });
            }
        };
    }

    document.addEventListener('DOMContentLoaded', function () {
        try {
            const { token, userId, userName } = getAuth();
            const uidEl = qs('meUserId');
            const unameEl = qs('meUserName');
            if (uidEl) uidEl.textContent = userId || '-';
            if (unameEl) unameEl.textContent = userName || '-';

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

            connectWS(token);
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
