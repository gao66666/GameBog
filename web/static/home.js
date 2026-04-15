(function () {
    const { qs, api } = window.GoBlog;

    function fmtTime(iso) {
        try {
            const d = new Date(iso);
            if (isNaN(d.getTime())) return '';
            return d.toLocaleString();
        } catch { return ''; }
    }

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

    async function loadLeaderboard() {
        const listEl = qs('leaderboard');
        const emptyEl = qs('leaderboardEmpty');

        try {
            const resp = await api('/api/v1/articles/leaderboard?type=view');
            const list = resp && resp.data && resp.data.article_list || [];
            listEl.innerHTML = '';

            if (!list.length) {
                emptyEl.style.display = '';
                return;
            }
            emptyEl.style.display = 'none';

            list.forEach((a) => {
                const li = document.createElement('li');
                const id = a.id;
                li.appendChild(articleLink(id, a.title));
                const meta = document.createElement('div');
                meta.className = 'muted';
                meta.innerHTML = renderStats(a.viewCount, a.likeCount, a.commentCount);
                if (meta.innerHTML) li.appendChild(meta);
                listEl.appendChild(li);
            });
        } catch (e) {
            listEl.innerHTML = '';
            emptyEl.style.display = '';
            emptyEl.textContent = '加载失败：' + e.message;
        }
    }

    async function loadLatest() {
        const root = qs('latest');
        const emptyEl = qs('latestEmpty');

        try {
            const resp = await api('/api/v1/articles/latest?page=1&size=10');
            const list = resp && resp.data && resp.data.article_list || [];
            root.innerHTML = '';

            if (!list.length) {
                emptyEl.style.display = '';
                return;
            }
            emptyEl.style.display = 'none';

            list.forEach((a) => {
                const div = document.createElement('div');
                div.className = 'item';

                const title = document.createElement('div');
                title.appendChild(articleLink(a.id, a.title));

                const summary = document.createElement('div');
                summary.className = 'muted';
                summary.textContent = (a.summary || '').slice(0, 160);

                const meta = document.createElement('div');
                meta.className = 'muted';
                const prefix = fmtTime(a.createdAt) ? (fmtTime(a.createdAt) + ' · ') : '';
                const stats = renderStats(a.viewCount, a.likeCount, a.commentCount);
                meta.innerHTML = prefix + stats;

                div.appendChild(title);
                if (summary.textContent) div.appendChild(summary);
                if (meta.innerHTML) div.appendChild(meta);
                root.appendChild(div);
            });
        } catch (e) {
            root.innerHTML = '';
            emptyEl.style.display = '';
            emptyEl.textContent = '加载失败：' + e.message;
        }
    }

    document.addEventListener('DOMContentLoaded', function () {
        loadLeaderboard();
        loadLatest();
    });
})();
