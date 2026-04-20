(function () {
    const { qs, api, getAuth } = window.GoBlog;

    function setText(id, t) {
        const el = qs(id);
        if (el) el.textContent = t || '';
    }

    function escapeText(s) {
        return String(s || '');
    }

    function fmtExpires(expiresAt) {
        if (!expiresAt) return '';
        try {
            const d = new Date(expiresAt);
            if (isNaN(d.getTime())) return '';
            return d.toLocaleString();
        } catch { return ''; }
    }


    let topicPage = 1;
    let topicTotal = 0;
    const topicPageSize = 5;

    function renderTopics(list, total) {
        const root = qs('topicList');
        const msg = qs('topicListMsg');
        const pager = qs('topicPager');
        if (!root) return;
        root.innerHTML = '';
        if (pager) pager.innerHTML = '';

        const topics = Array.isArray(list) ? list : [];
        if (!topics.length) {
            if (msg) msg.textContent = '暂无话题';
            return;
        }
        if (msg) msg.textContent = '';

        topics.forEach((t) => {
            const id = t && (t.id ?? t.ID);
            const name = String((t && (t.name ?? t.Name)) || '').trim();
            if (!id || !name) return;
            const isTemp = !!(t && (t.isTemporary ?? t.is_temporary ?? t.IsTemporary));
            const exp = t && (t.expiresAt ?? t.expires_at ?? t.ExpiresAt);

            const row = document.createElement('div');
            row.className = 'item';
            row.style.padding = '14px 0';

            const a = document.createElement('a');
            a.href = '/topic/' + encodeURIComponent(String(id));
            a.textContent = name;
            a.style.display = 'inline-block';
            a.style.fontSize = '18px';
            a.style.fontWeight = '700';

            const meta = document.createElement('div');
            meta.className = 'muted';
            meta.style.marginTop = '8px';
            meta.textContent = isTemp ? ('临时 · 到期 ' + (fmtExpires(exp) || '-')) : '长期';

            row.appendChild(a);
            row.appendChild(meta);
            root.appendChild(row);
        });

        // 分页控件
        if (pager && total > topicPageSize) {
            const totalPages = Math.ceil(total / topicPageSize);
            if (topicPage > 1) {
                const prev = document.createElement('button');
                prev.textContent = '上一页';
                prev.onclick = function () { topicPage--; loadTopics(); };
                pager.appendChild(prev);
            }
            pager.appendChild(document.createTextNode(' 第 ' + topicPage + ' / ' + totalPages + ' 页 '));
            if (topicPage < totalPages) {
                const next = document.createElement('button');
                next.textContent = '下一页';
                next.onclick = function () { topicPage++; loadTopics(); };
                pager.appendChild(next);
            }
        }
    }

    async function loadTopics() {
        setText('topicListMsg', '加载中...');
        try {
            const resp = await api('/api/v1/topics?page=' + topicPage + '&size=' + topicPageSize);
            const list = resp && resp.data && (resp.data.topics || resp.data.list || resp.data) || [];
            topicTotal = (resp && resp.data && (resp.data.total || resp.data.count)) || 0;
            renderTopics(list, topicTotal);
        } catch (e) {
            setText('topicListMsg', '加载失败：' + e.message);
        }
    }

    async function createTopic(ev) {
        ev.preventDefault();

        const { token } = getAuth();
        if (!token) {
            setText('topicCreateMsg', '请先登录再创建话题');
            return;
        }

        const input = qs('topicNameInput');
        const name = String((input && input.value) || '').trim();
        if (!name) {
            setText('topicCreateMsg', '请输入话题名称');
            return;
        }

        const kindEl = document.querySelector('input[name="topicKind"]:checked');
        const kind = kindEl ? String(kindEl.value || 'long') : 'long';

        setText('topicCreateMsg', '创建中...');
        try {
            await api('/api/v1/topics', {
                method: 'POST',
                body: JSON.stringify({ name: escapeText(name), kind })
            });
            setText('topicCreateMsg', '创建成功');
            if (input) input.value = '';
            await loadTopics();
        } catch (e) {
            setText('topicCreateMsg', '创建失败：' + e.message);
        }
    }

    document.addEventListener('DOMContentLoaded', function () {
        const form = qs('topicCreateForm');
        if (form) form.addEventListener('submit', createTopic);
        loadTopics();
    });
})();
