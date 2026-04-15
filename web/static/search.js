(function () {
    const { qs, api } = window.GoBlog;

    function articleLink(id, title) {
        const a = document.createElement('a');
        a.href = '/article/' + encodeURIComponent(id);
        a.textContent = title || ('文章 ' + id);
        return a;
    }

    function toNum(v) {
        const n = Number(v);
        return Number.isFinite(n) ? n : 0;
    }

    function buildStatsLine(a) {
        const view = toNum(a.viewCount ?? a.view_count ?? a.viewCount);
        const like = toNum(a.likeCount ?? a.like_count ?? a.likeCount);
        const comment = toNum(a.commentCount ?? a.comment_count ?? a.commentCount);

        const parts = [];
        // 需求理解：没有（0）就不显示
        if (view > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">👁</span>' + view + '</span>');
        if (like > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">👍</span>' + like + '</span>');
        if (comment > 0) parts.push('<span class="stat"><span class="stat-icon" aria-hidden="true">💬</span>' + comment + '</span>');

        return parts.join(' · ');
    }

    function renderSearchResults(data, keyword) {
        const section = qs('searchSection');
        const root = qs('searchResults');
        const msg = qs('searchMsg');
        const artRoot = qs('searchArticles');
        const userRoot = qs('searchUsers');

        if (!root || !msg || !artRoot || !userRoot) return;

        if (section) {
            section.style.display = '';
        }

        const articles = data && data.articles || [];
        const users = data && data.users || [];

        artRoot.innerHTML = '';
        userRoot.innerHTML = '';

        if ((!articles || !articles.length) && (!users || !users.length)) {
            msg.textContent = '没有找到与 “' + keyword + '” 相关的结果';
            root.style.display = 'none';
            return;
        }

        msg.textContent = '';
        root.style.display = '';

        (articles || []).forEach((a) => {
            const div = document.createElement('div');
            div.className = 'item';

            const title = document.createElement('div');
            const id = a.id || a.ID;
            title.appendChild(articleLink(id, a.title || a.Title));

            const summary = document.createElement('div');
            summary.className = 'muted';
            const s = a.summary || a.Summary || '';
            summary.textContent = String(s).slice(0, 160);

            const meta = document.createElement('div');
            meta.className = 'muted';
            meta.innerHTML = buildStatsLine(a);

            div.appendChild(title);
            if (summary.textContent) div.appendChild(summary);
            if (meta.innerHTML) div.appendChild(meta);
            artRoot.appendChild(div);
        });

        (users || []).forEach((u) => {
            const div = document.createElement('div');
            div.className = 'item row';
            const left = document.createElement('div');
            left.style.flex = '1 1 auto';

            const name = document.createElement('div');
            name.textContent = u.name || u.Name || ('用户 ' + (u.id || u.ID || ''));

            const meta = document.createElement('div');
            meta.className = 'muted';
            const tel = u.tel || u.Tel || '';
            const email = u.email || u.Email || '';
            const parts = [];
            if (tel) parts.push('电话 ' + tel);
            if (email) parts.push('邮箱 ' + email);
            meta.textContent = parts.join(' · ');

            left.appendChild(name);
            if (meta.textContent) left.appendChild(meta);

            div.appendChild(left);
            userRoot.appendChild(div);
        });
    }

    document.addEventListener('DOMContentLoaded', function () {
        const form = qs('searchForm');
        const input = qs('searchInput');
        const msg = qs('searchMsg');
        const root = qs('searchResults');
        const section = qs('searchSection');
        const closeBtn = qs('searchClose');

        if (!form || !input || !msg || !root) return;

        if (closeBtn) {
            closeBtn.addEventListener('click', function () {
                if (section) section.style.display = 'none';
                root.style.display = 'none';
                msg.textContent = '';
            });
        }

        form.addEventListener('submit', async function (ev) {
            ev.preventDefault();
            const q = String(input.value || '').trim();
            if (!q) {
                msg.textContent = '请输入要搜索的关键词';
                root.style.display = 'none';
                if (section) section.style.display = 'none';
                return;
            }
            msg.textContent = '搜索中...';
            root.style.display = 'none';
            if (section) section.style.display = '';
            try {
                const resp = await api('/api/v1/search?q=' + encodeURIComponent(q));
                renderSearchResults(resp && resp.data, q);
            } catch (e) {
                msg.textContent = '搜索失败：' + e.message;
                root.style.display = 'none';
                if (section) section.style.display = '';
            }
        });
    });
})();
