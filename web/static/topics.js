(function () {
    'use strict';

    if (!window.GoBlog) return;
    var qs = window.GoBlog.qs;
    var api = window.GoBlog.api;
    var getAuth = window.GoBlog.getAuth;

    var state = { page: 1, size: 10, total: 0 };

    function setText(id, t) { var el = qs(id); if (el) el.textContent = t || ''; }

    function fmtExpires(exp) {
        if (!exp) return '';
        try {
            var d = new Date(exp);
            if (isNaN(d.getTime())) return '';
            return '到期 ' + d.toLocaleString();
        } catch (e) { return ''; }
    }

    function render(list, total) {
        var root = qs('topicList');
        var empty = qs('topicEmpty');
        var count = qs('topicCount');
        if (!root) return;
        root.innerHTML = '';

        var topics = Array.isArray(list) ? list : [];

        if (count) count.textContent = total ? ('共 ' + total + ' 个话题') : '暂无话题';

        if (!topics.length) {
            if (empty) empty.style.display = '';
            return;
        }
        if (empty) empty.style.display = 'none';

        topics.forEach(function (t) {
            var id = t && (t.id || t.ID);
            var name = String(t && (t.name || t.Name) || '').trim();
            if (!id || !name) return;

            var isTemp = !!(t && (t.isTemporary || t.is_temporary || t.IsTemporary));
            var exp = t && (t.expiresAt || t.expires_at || t.ExpiresAt);

            var card = document.createElement('div');
            card.className = 'topic-card';

            var body = document.createElement('div');
            body.className = 'topic-card-body';

            var title = document.createElement('a');
            title.className = 'topic-card-title';
            title.href = '/topic/' + encodeURIComponent(String(id));
            title.textContent = name;
            body.appendChild(title);

            var meta = document.createElement('div');
            meta.className = 'topic-card-meta';

            var badge = document.createElement('span');
            badge.className = 'topic-card-badge ' + (isTemp ? 'badge-temporary' : 'badge-permanent');
            badge.textContent = isTemp ? '临时' : '长期';
            meta.appendChild(badge);

            if (isTemp && exp) {
                var expires = document.createElement('span');
                expires.className = 'topic-card-expires';
                expires.textContent = fmtExpires(exp);
                meta.appendChild(expires);
            }

            body.appendChild(meta);
            card.appendChild(body);
            root.appendChild(card);
        });

        // 分页
        var prevBtn = qs('topicPrev');
        var nextBtn = qs('topicNext');
        var info = qs('topicPageInfo');
        if (!prevBtn || !nextBtn) return;

        var maxPage = Math.max(1, Math.ceil(total / state.size));
        if (info) info.textContent = total > 0 ? ('第 ' + state.page + ' / ' + maxPage + ' 页') : '';
        prevBtn.disabled = state.page <= 1;
        nextBtn.disabled = state.page >= maxPage || total === 0;
    }

    async function load() {
        var root = qs('topicList');
        if (root) root.innerHTML = '';
        setText('topicCount', '加载中...');

        try {
            var resp = await api('/api/v1/topics?page=' + state.page + '&size=' + state.size);
            var data = resp && resp.data;
            var list = (data && (data.topics || data.list || data)) || [];
            state.total = (data && (data.total || data.count)) || 0;
            render(list, state.total);
        } catch (e) {
            setText('topicCount', '加载失败');
            var empty = qs('topicEmpty');
            if (empty) { empty.style.display = ''; empty.innerHTML = '<div class="topics-empty-icon">⚠</div><div class="topics-empty-text">' + e.message + '</div>'; }
        }
    }

    async function createTopic() {
        var auth = getAuth();
        if (!auth.token) {
            setText('topicCreateMsg', '请先登录');
            return;
        }
        var input = qs('topicNameInput');
        var name = String(input && input.value || '').trim();
        if (!name) { setText('topicCreateMsg', '请输入话题名称'); return; }

        var kindEl = document.querySelector('input[name="topicKind"]:checked');
        var kind = kindEl ? String(kindEl.value || 'long') : 'long';

        var btn = qs('topicCreateBtn');
        if (btn) btn.disabled = true;
        setText('topicCreateMsg', '创建中...');

        try {
            await api('/api/v1/topics', {
                method: 'POST',
                body: JSON.stringify({ name: name, kind: kind })
            });
            setText('topicCreateMsg', '✅ 创建成功');
            if (input) input.value = '';
            state.page = 1;
            await load();
        } catch (e) {
            setText('topicCreateMsg', '❌ ' + e.message);
        } finally {
            if (btn) btn.disabled = false;
        }
    }

    function init() {
        load();

        var createBtn = qs('topicCreateBtn');
        if (createBtn) createBtn.addEventListener('click', createTopic);

        var prevBtn = qs('topicPrev');
        var nextBtn = qs('topicNext');
        if (prevBtn) prevBtn.addEventListener('click', function () { if (state.page > 1) { state.page--; load(); } });
        if (nextBtn) nextBtn.addEventListener('click', function () {
            var maxPage = Math.ceil(state.total / state.size);
            if (state.page < maxPage) { state.page++; load(); }
        });

        var input = qs('topicNameInput');
        if (input) {
            input.addEventListener('keydown', function (e) {
                if (e.key === 'Enter') { e.preventDefault(); createTopic(); }
            });
        }
    }

    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
    else init();
})();
