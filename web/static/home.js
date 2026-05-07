(function () {
    const { qs, api, getAuth } = window.GoBlog;

    const DEFAULT_ARTICLE_COVER = 'https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg';

    function pickArticleCoverUrl(a) {
        const u = String((a && (a.coverUrl || a.cover_url)) || '').trim();
        return /^https?:\/\//i.test(u) ? u : DEFAULT_ARTICLE_COVER;
    }

    function fmtTime(iso) {
        try {
            const d = new Date(iso);
            if (isNaN(d.getTime())) return '';
            return d.toLocaleString();
        } catch {
            return '';
        }
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
        if (!listEl || !emptyEl) return;

        try {
            const resp = await api('/api/v1/articles/leaderboard?type=view');
            const list = (resp && resp.data && resp.data.article_list) || [];
            listEl.innerHTML = '';

            if (!list.length) {
                emptyEl.style.display = '';
                return;
            }
            emptyEl.style.display = 'none';

            list.forEach((a, idx) => {
                const li = document.createElement('li');
                li.className = 'home-lb-row';
                const rank = document.createElement('span');
                rank.className = 'home-lb-rank';
                rank.textContent = String(idx + 1);
                const body = document.createElement('div');
                body.className = 'home-lb-body';
                body.appendChild(articleLink(a.id, a.title));
                const meta = document.createElement('div');
                meta.className = 'home-lb-meta';
                meta.innerHTML = renderStats(a.viewCount, a.likeCount, a.commentCount);
                if (meta.innerHTML) body.appendChild(meta);
                li.appendChild(rank);
                li.appendChild(body);
                listEl.appendChild(li);
            });
        } catch (e) {
            listEl.innerHTML = '';
            emptyEl.style.display = '';
            emptyEl.textContent = '加载失败：' + (e && e.message || '');
        }
    }

    // 最新文章
    let latestScope = 'all'; // all | follow
    let latestPage = 1;
    const latestSize = 6;
    let latestTotal = 0;

    function setToggleActive(scope) {
        const allBtn = qs('latestAllBtn');
        const followBtn = qs('latestFollowBtn');
        if (allBtn) allBtn.classList.toggle('toggle-active', scope === 'all');
        if (followBtn) followBtn.classList.toggle('toggle-active', scope === 'follow');
    }

    function maxLatestPage() {
        const raw = Math.max(1, Math.ceil((latestTotal || 0) / latestSize));
        // “全部”只做 3 页（18 条）
        return latestScope === 'all' ? Math.min(3, raw) : raw;
    }

    function updateLatestPager() {
        const pager = qs('latestPager');
        const prevBtn = qs('latestPrev');
        const nextBtn = qs('latestNext');
        const info = qs('latestPageInfo');

        const maxPage = maxLatestPage();
        if (latestPage > maxPage) latestPage = maxPage;

        if (prevBtn) prevBtn.disabled = latestPage <= 1;
        if (nextBtn) nextBtn.disabled = latestPage >= maxPage;
        if (info) info.textContent = '第 ' + latestPage + ' / ' + maxPage + ' 页';

        // 少于 6 条（仅 1 页）就不展示分页控件
        if (pager) {
            pager.style.display = (latestTotal > latestSize) ? '' : 'none';
        }
    }

    function refreshFollowBtnState() {
        const toggle = qs('latestToggle');
        const followBtn = qs('latestFollowBtn');
        if (toggle) toggle.style.display = '';
        if (!followBtn) return;

        const { token } = getAuth();
        followBtn.disabled = !token;
        followBtn.title = token ? '' : '登录后可用';
    }

    async function loadLatest() {
        const root = qs('latest');
        const emptyEl = qs('latestEmpty');
        if (!root || !emptyEl) return;

        refreshFollowBtnState();
        setToggleActive(latestScope);

        root.innerHTML = '';
        emptyEl.style.display = '';
        emptyEl.textContent = '加载中...';

        // “仅关注”需要登录态
        if (latestScope === 'follow') {
            const { token } = getAuth();
            if (!token) {
                latestScope = 'all';
                latestPage = 1;
                latestTotal = 0;
                setToggleActive(latestScope);
                emptyEl.textContent = '登录后可查看“仅关注”';
                updateLatestPager();
                return;
            }
        }

        try {
            const endpoint = (latestScope === 'follow')
                ? ('/api/v1/articles/following_latest?page=' + latestPage + '&size=' + latestSize)
                : ('/api/v1/articles/latest?page=' + latestPage + '&size=' + latestSize);

            const resp = await api(endpoint);
            const list = (resp && resp.data && resp.data.article_list) || [];
            latestTotal = Number(resp && resp.data && resp.data.total || 0) || 0;

            if (latestScope === 'all' && latestTotal > 18) {
                latestTotal = 18;
            }

            if (!list.length) {
                emptyEl.style.display = '';
                emptyEl.textContent = '暂无文章';
                updateLatestPager();
                return;
            }

            emptyEl.style.display = 'none';

            list.forEach((a) => {
                const div = document.createElement('div');
                div.className = 'home-feed-item';

                const thumb = document.createElement('img');
                thumb.className = 'home-feed-thumb';
                thumb.src = pickArticleCoverUrl(a);
                thumb.alt = '';
                thumb.loading = 'lazy';
                div.appendChild(thumb);

                const main = document.createElement('div');
                main.className = 'home-feed-main';

                const title = document.createElement('div');
                title.className = 'home-feed-title';
                title.appendChild(articleLink(a.id, a.title));

                const summary = document.createElement('div');
                summary.className = 'home-feed-summary';
                summary.textContent = String(a.summary || '').slice(0, 200);

                const meta = document.createElement('div');
                meta.className = 'home-feed-meta';
                const t = fmtTime(a.createdAt);
                const prefix = t ? (t + ' · ') : '';
                const stats = renderStats(a.viewCount, a.likeCount, a.commentCount);
                meta.innerHTML = prefix + stats;

                main.appendChild(title);
                if (summary.textContent) main.appendChild(summary);
                if (meta.innerHTML) main.appendChild(meta);
                div.appendChild(main);
                root.appendChild(div);
            });

            updateLatestPager();
        } catch (e) {
            latestTotal = 0;
            root.innerHTML = '';
            emptyEl.style.display = '';
            emptyEl.textContent = '加载失败：' + (e && e.message || '');
            updateLatestPager();
        }
    }

    function setupLatestControls() {
        const toggle = qs('latestToggle');
        const allBtn = qs('latestAllBtn');
        const followBtn = qs('latestFollowBtn');
        const prevBtn = qs('latestPrev');
        const nextBtn = qs('latestNext');

        if (toggle) toggle.style.display = '';
        refreshFollowBtnState();

        if (allBtn) {
            allBtn.addEventListener('click', function () {
                if (latestScope === 'all') return;
                latestScope = 'all';
                latestPage = 1;
                loadLatest();
            });
        }

        if (followBtn) {
            followBtn.addEventListener('click', function () {
                if (latestScope === 'follow') return;
                const { token } = getAuth();
                if (!token) return;
                latestScope = 'follow';
                latestPage = 1;
                loadLatest();
            });
        }

        if (prevBtn) {
            prevBtn.addEventListener('click', function () {
                if (latestPage <= 1) return;
                latestPage -= 1;
                loadLatest();
            });
        }

        if (nextBtn) {
            nextBtn.addEventListener('click', function () {
                const maxPage = maxLatestPage();
                if (latestPage >= maxPage) return;
                latestPage += 1;
                loadLatest();
            });
        }
    }

    document.addEventListener('DOMContentLoaded', function () {
        loadLeaderboard();
        setupLatestControls();
        loadLatest();
    });
})();
