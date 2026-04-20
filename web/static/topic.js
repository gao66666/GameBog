(function () {
    const { qs, api, getAuth } = window.GoBlog;

    function pageEl() { return qs('topicPage'); }
    function getTopicId() {
        const el = pageEl();
        return (el && el.getAttribute('data-topic-id')) || '';
    }

    function setText(id, text) {
        const el = qs(id);
        if (el) el.textContent = text || '';
    }

    function fmtTime(iso) {
        try {
            const d = new Date(iso);
            if (isNaN(d.getTime())) return '';
            return d.toLocaleString();
        } catch { return ''; }
    }

    async function loadTopic() {
        const id = getTopicId();
        const resp = await api('/api/v1/topics/' + encodeURIComponent(String(id)));
        const t = resp && resp.data && resp.data.topic;
        if (!t) throw new Error('话题不存在');

        const name = String((t.name ?? t.Name) || '').trim() || ('话题 #' + id);
        setText('topicTitle', name);

        const isTemp = !!(t.isTemporary ?? t.is_temporary ?? t.IsTemporary);
        const exp = t.expiresAt ?? t.expires_at ?? t.ExpiresAt;
        const meta = isTemp ? ('临时 · 到期 ' + (fmtTime(exp) || String(exp || '-'))) : '长期';
        setText('topicMeta', meta);

        // 控制删除按钮显示
        const delBtn = qs('topicDeleteBtn');
        if (delBtn) {
            delBtn.style.display = isTemp ? '' : 'none';
            delBtn.onclick = async function () {
                if (!confirm('确定要删除该临时话题及其所有内容吗？')) return;
                try {
                    await api('/api/v1/topics/' + encodeURIComponent(String(id)), { method: 'DELETE' });
                    alert('删除成功');
                    window.location.href = '/topics';
                } catch (e) {
                    alert('删除失败：' + (e && e.message || e));
                }
            };
        }
    }

    function renderArticles(list) {
        const root = qs('topicArticles');
        const empty = qs('topicArticlesEmpty');
        if (!root) return;
        root.innerHTML = '';

        const articles = Array.isArray(list) ? list : [];
        if (!articles.length) {
            if (empty) empty.style.display = '';
            return;
        }
        if (empty) empty.style.display = 'none';

        articles.forEach((a) => {
            if (!a) return;
            const id = a.id || a.ID || a.article_id;
            const title = String(a.title || a.Title || '').trim() || ('文章 ' + id);
            const summary = String(a.summary || a.Summary || '').trim();
            if (!id) return;

            const item = document.createElement('div');
            item.className = 'item';

            const link = document.createElement('a');
            link.href = '/article/' + encodeURIComponent(String(id));
            link.textContent = title;

            item.appendChild(link);

            if (summary) {
                const s = document.createElement('div');
                s.className = 'muted';
                s.style.marginTop = '6px';
                s.textContent = summary;
                item.appendChild(s);
            }

            root.appendChild(item);
        });

        if (!root.childNodes.length && empty) {
            empty.style.display = '';
        }
    }

    async function loadArticles() {
        const id = getTopicId();
        const resp = await api('/api/v1/topics/' + encodeURIComponent(String(id)) + '/articles?page=1&size=30');
        const data = resp && resp.data;
        const list = data && (data.article_list || data.list || data) || [];
        renderArticles(list);
    }

    function renderDiscussions(list) {
        const root = qs('topicDiscussions');
        const empty = qs('topicDiscussionsEmpty');
        if (!root) return;
        root.innerHTML = '';

        const me = getAuth();
        const myId = String(me.userId || '');

        const rows = Array.isArray(list) ? list : [];
        if (!rows.length) {
            if (empty) empty.style.display = '';
            return;
        }
        if (empty) empty.style.display = 'none';

        rows.forEach((d) => {
            if (!d) return;

            const uid = String(d.userId || d.user_id || '');
            const isMe = myId && uid && uid === myId;

            const line = document.createElement('div');
            line.className = 'chat-line ' + (isMe ? 'chat-me' : 'chat-other');

            const name = String(d.userName || d.user_name || '').trim() || '匿名';
            const myName = String(me.userName || '').trim();
            const showName = isMe ? (myName || '我') : name;

            const n = document.createElement('div');
            n.className = 'chat-name';
            n.textContent = showName;
            line.appendChild(n);

            const bubble = document.createElement('div');
            bubble.className = 'chat-bubble';
            bubble.textContent = String(d.content || '').trim();
            line.appendChild(bubble);

            const time = document.createElement('div');
            time.className = 'chat-time';
            const created = d.createdAt || d.created_at || '';
            time.textContent = created ? (fmtTime(created) || String(created)) : '';
            line.appendChild(time);

            root.appendChild(line);
        });

        if (!root.childNodes.length && empty) {
            empty.style.display = '';
        }
    }

    function isNearBottom(box) {
        if (!box) return true;
        const gap = box.scrollHeight - box.scrollTop - box.clientHeight;
        return gap < 40;
    }

    async function loadDiscussions() {
        const id = getTopicId();
        const box = qs('topicChatBox');
        const keepBottom = isNearBottom(box);

        const resp = await api('/api/v1/topics/' + encodeURIComponent(String(id)) + '/discussions?page=1&size=50');
        const data = resp && resp.data;
        const list = data && (data.list || data.rows || data) || [];
        const rows = Array.isArray(list) ? list.slice().reverse() : [];
        renderDiscussions(rows);

        if (box && keepBottom) {
            box.scrollTop = box.scrollHeight;
        }
    }

    async function submitDiscussion(ev) {
        ev.preventDefault();

        const { token } = getAuth();
        const msg = qs('topicDiscussionMsg');
        if (msg) msg.textContent = '';

        if (!token) {
            if (msg) msg.textContent = '请先登录再发表讨论';
            return;
        }

        const input = qs('topicDiscussionInput');
        const content = String((input && input.value) || '').trim();
        if (!content) {
            if (msg) msg.textContent = '内容不能为空';
            return;
        }

        const id = getTopicId();
        try {
            await api('/api/v1/topics/' + encodeURIComponent(String(id)) + '/discussions', {
                method: 'POST',
                body: JSON.stringify({ content })
            });
            if (input) input.value = '';
            if (msg) msg.textContent = '已发表';
            await loadDiscussions();
        } catch (e) {
            if (msg) msg.textContent = '发表失败：' + e.message;
        }
    }

    document.addEventListener('DOMContentLoaded', async function () {
        const form = qs('topicDiscussionForm');
        if (form) form.addEventListener('submit', submitDiscussion);

        const input = qs('topicDiscussionInput');
        if (form && input) {
            input.addEventListener('keydown', function (ev) {
                // Enter 发送；Shift+Enter 换行
                if (ev.key !== 'Enter') return;
                if (ev.shiftKey) return;
                // 中文输入法合成态：按回车选词时不发送
                if (ev.isComposing || ev.keyCode === 229) return;

                ev.preventDefault();
                if (form.requestSubmit) {
                    form.requestSubmit();
                } else {
                    submitDiscussion({ preventDefault: function () { } });
                }
            });
        }

        try {
            await loadTopic();
        } catch (e) {
            setText('topicTitle', '加载失败');
            setText('topicMeta', e.message);
            return;
        }

        if (qs('topicArticles')) {
            try { await loadArticles(); } catch { /* ignore */ }
        }
        if (qs('topicDiscussions')) {
            try { await loadDiscussions(); } catch { /* ignore */ }

            const pollId = window.setInterval(function () {
                if (document.hidden) return;
                loadDiscussions().catch(function () { });
            }, 5000);
            window.addEventListener('beforeunload', function () {
                try { window.clearInterval(pollId); } catch { /* ignore */ }
            });
        }
    });
})();
