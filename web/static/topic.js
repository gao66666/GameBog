(function () {
    'use strict';

    if (!window.GoBlog) return;
    var qs = window.GoBlog.qs;
    var api = window.GoBlog.api;
    var getAuth = window.GoBlog.getAuth;

    function pageEl() { return qs('topicPage'); }
    function getTopicId() {
        var el = pageEl();
        return (el && el.getAttribute('data-topic-id')) || '';
    }

    function setText(id, t) { var el = qs(id); if (el) el.textContent = t || ''; }
    function setHTML(id, h) { var el = qs(id); if (el) el.innerHTML = h; }
    function setVisible(id, show) { var el = qs(id); if (el) el.style.display = show ? '' : 'none'; }

    function escapeHtml(s) {
        return String(s || '')
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    }

    var topicFollowState = { followed: false };

    function fmtTime(iso) {
        if (!iso) return '';
        try { var d = new Date(iso); if (isNaN(d.getTime())) return ''; return d.toLocaleString(); }
        catch (e) { return ''; }
    }

    function firstChar(t) {
        var s = String(t || '').trim();
        return s ? s.charAt(0).toUpperCase() : '?';
    }

    var DEFAULT_ARTICLE_COVER = 'https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg';
    function pickArticleCoverUrl(a) {
        var u = String((a && (a.coverUrl || a.cover_url)) || '').trim();
        return /^https?:\/\//i.test(u) ? u : DEFAULT_ARTICLE_COVER;
    }

    // ============================================================
    // 话题基本信息
    // ============================================================

    function applyTopicFollowUI() {
        var btn = qs('topicFollowBtn');
        var hint = qs('topicFollowHint');
        var txt = qs('topicFollowBtnText');
        if (hint) hint.textContent = '';
        if (!btn) return;

        btn.style.display = 'inline-flex';
        btn.classList.toggle('is-active', topicFollowState.followed);
        btn.setAttribute('aria-pressed', topicFollowState.followed ? 'true' : 'false');
        if (txt) {
            txt.textContent = topicFollowState.followed ? '已关注' : '关注话题';
        }
    }

    function wireTopicFollow() {
        var btn = qs('topicFollowBtn');
        if (!btn || btn.getAttribute('data-wired') === '1') return;
        btn.setAttribute('data-wired', '1');
        btn.addEventListener('click', async function () {
            var hint = qs('topicFollowHint');
            var tid = getTopicId();
            if (!tid) return;

            var auth = getAuth();
            if (!auth.token) {
                if (hint) hint.textContent = '请先登录';
                return;
            }

            btn.disabled = true;
            if (hint) hint.textContent = '';

            try {
                if (topicFollowState.followed) {
                    await api('/api/v1/follow/topic', {
                        method: 'DELETE',
                        body: JSON.stringify({ topicId: String(tid) })
                    });
                    topicFollowState.followed = false;
                    if (hint) hint.textContent = '已取消关注';
                } else {
                    await api('/api/v1/follow/topic', {
                        method: 'POST',
                        body: JSON.stringify({ topicId: String(tid) })
                    });
                    topicFollowState.followed = true;
                    if (hint) hint.textContent = '已关注，可在个人中心查看';
                }
                applyTopicFollowUI();
            } catch (e) {
                if (hint) hint.textContent = (e && e.message) ? String(e.message) : '操作失败';
            } finally {
                btn.disabled = false;
            }
        });
    }

    async function loadTopic() {
        var id = getTopicId();
        var resp = await api('/api/v1/topics/' + encodeURIComponent(String(id)));
        var data = resp && resp.data;
        var t = data && data.topic;
        if (!t) throw new Error('话题不存在');

        topicFollowState.followed = !!(data && (data.isTopicFollowed === true || data.isTopicFollowed === true));

        var name = String((t.name || t.Name) || '').trim() || ('话题 #' + id);
        setText('topicTitle', name);

        var isTemp = !!(t.isTemporary || t.is_temporary || t.IsTemporary);
        var exp = t.expiresAt || t.expires_at || t.ExpiresAt;

        var badge = qs('topicBadge');
        if (badge) {
            badge.textContent = isTemp ? '临时' : '长期';
            badge.className = 'topic-detail-badge ' + (isTemp ? 'badge-temporary' : 'badge-permanent');
            setVisible('topicBadge', true);
        }

        var metaEl = qs('topicMeta');
        var count = t.article_count || t.articleCount || 0;
        var pills = [];
        if (isTemp && exp) {
            pills.push('<span class="pill">⏱ ' + escapeHtml(fmtTime(exp) || String(exp)) + '</span>');
        }
        pills.push('<span class="pill">📄 ' + String(count) + ' 篇文章</span>');
        if (metaEl) metaEl.innerHTML = pills.join('');

        applyTopicFollowUI();
        wireTopicFollow();

        // 删除按钮
        var delBtn = qs('topicDeleteBtn');
        if (delBtn) {
            delBtn.style.display = isTemp ? 'inline-flex' : 'none';
            delBtn.onclick = async function () {
                if (!confirm('确定删除该临时话题及其所有内容吗？')) return;
                try {
                    await api('/api/v1/topics/' + encodeURIComponent(String(id)), { method: 'DELETE' });
                    window.location.href = '/topics';
                } catch (e) {
                    alert('删除失败：' + (e && e.message || e));
                }
            };
        }
    }

    // ============================================================
    // 文章列表
    // ============================================================

    function renderArticles(list) {
        var root = qs('topicArticles');
        var empty = qs('topicArticlesEmpty');
        if (!root) return;
        root.innerHTML = '';

        var articles = Array.isArray(list) ? list : [];
        if (!articles.length) {
            if (empty) empty.style.display = '';
            return;
        }
        if (empty) empty.style.display = 'none';

        articles.forEach(function (a) {
            if (!a) return;
            var id = a.id || a.ID || a.article_id;
            if (!id) return;
            var title = String(a.title || a.Title || '').trim() || ('文章 ' + id);
            var summary = String(a.summary || a.Summary || '').trim();

            var item = document.createElement('div');
            item.className = 'article-item article-item-row';

            var thumb = document.createElement('img');
            thumb.className = 'article-item-thumb';
            thumb.src = pickArticleCoverUrl(a);
            thumb.alt = '';
            thumb.loading = 'lazy';
            item.appendChild(thumb);

            var body = document.createElement('div');
            body.className = 'article-item-body';

            var link = document.createElement('a');
            link.className = 'article-item-title';
            link.href = '/article/' + encodeURIComponent(String(id));
            link.textContent = title;
            body.appendChild(link);

            if (summary) {
                var s = document.createElement('div');
                s.className = 'article-item-summary';
                s.textContent = summary;
                body.appendChild(s);
            }

            item.appendChild(body);
            root.appendChild(item);
        });
    }

    async function loadArticles() {
        var id = getTopicId();
        try {
            var resp = await api('/api/v1/topics/' + encodeURIComponent(String(id)) + '/articles?page=1&size=30');
            var data = resp && resp.data;
            var list = (data && (data.article_list || data.list || data)) || [];
            renderArticles(list);
        } catch (e) { /* ignore */ }
    }

    // ============================================================
    // 讨论区
    // ============================================================

    function renderDiscussions(list, totalHint) {
        var root = qs('topicDiscussions');
        var empty = qs('topicDiscussionsEmpty');
        var count = qs('discussCount');
        if (!root) return;
        root.innerHTML = '';

        var me = getAuth();
        var myId = String(me.userId || '');

        var rows = Array.isArray(list) ? list : [];
        var total = typeof totalHint === 'number' && totalHint >= 0 ? totalHint : rows.length;

        if (!rows.length) {
            if (empty) empty.style.display = '';
            if (count) count.textContent = '';
            return;
        }
        if (empty) empty.style.display = 'none';
        if (count) {
            count.textContent = total > rows.length
                ? '（本页 ' + rows.length + ' 条 · 共 ' + total + ' 条，上下滚动查看）'
                : '（共 ' + total + ' 条，上下滚动查看更早消息）';
        }

        rows.forEach(function (d) {
            if (!d) return;
            var uid = String(d.userId || d.user_id || '');
            var isMe = myId && uid && uid === myId;

            var item = document.createElement('div');
            item.className = 'discuss-item ' + (isMe ? 'discuss-item-me' : 'discuss-item-other');

            var avatar = document.createElement('div');
            avatar.className = 'discuss-avatar ' + (isMe ? 'discuss-avatar-me' : 'discuss-avatar-other');
            avatar.textContent = firstChar(d.userName || d.user_name);
            item.appendChild(avatar);

            var body = document.createElement('div');
            body.className = 'discuss-body';

            var bubble = document.createElement('div');
            bubble.className = 'discuss-bubble';

            var header = document.createElement('div');
            header.className = 'discuss-header';

            var name = document.createElement('span');
            name.className = 'discuss-name ' + (isMe ? 'discuss-name-me' : '');
            name.textContent = (isMe ? (me.userName || '我') : (d.userName || d.user_name || '匿名'));
            header.appendChild(name);

            var time = document.createElement('span');
            time.className = 'discuss-time';
            time.textContent = fmtTime(d.createdAt || d.created_at) || '';
            header.appendChild(time);

            bubble.appendChild(header);

            var content = document.createElement('div');
            content.className = 'discuss-content';
            content.textContent = String(d.content || '').trim();
            bubble.appendChild(content);

            body.appendChild(bubble);
            item.appendChild(body);
            root.appendChild(item);
        });
    }

    function isNearBottom(box) {
        if (!box) return true;
        return box.scrollHeight - box.scrollTop - box.clientHeight < 40;
    }

    async function loadDiscussions() {
        var id = getTopicId();
        var box = qs('topicChatBox');
        var keepBottom = isNearBottom(box);

        try {
            var resp = await api('/api/v1/topics/' + encodeURIComponent(String(id)) + '/discussions?page=1&size=50');
            var data = resp && resp.data;
            var list = (data && (data.list || data.rows || data)) || [];
            var total = typeof (data && data.total) === 'number' ? data.total : (Array.isArray(list) ? list.length : 0);
            var rows = Array.isArray(list) ? list.slice().reverse() : [];
            renderDiscussions(rows, total);
            if (box && keepBottom) box.scrollTop = box.scrollHeight;
        } catch (e) { /* ignore */ }
    }

    async function submitDiscussion() {
        var auth = getAuth();
        var msg = qs('topicDiscussionMsg');
        if (msg) msg.textContent = '';

        if (!auth.token) {
            if (msg) msg.textContent = '请先登录';
            return;
        }

        var input = qs('topicDiscussionInput');
        var content = String((input && input.value) || '').trim();
        if (!content) {
            if (msg) msg.textContent = '内容不能为空';
            return;
        }

        var id = getTopicId();
        var btn = qs('discussSendBtn');
        if (btn) btn.disabled = true;

        try {
            await api('/api/v1/topics/' + encodeURIComponent(String(id)) + '/discussions', {
                method: 'POST',
                body: JSON.stringify({ content: content })
            });
            if (input) { input.value = ''; input.style.height = 'auto'; }
            if (msg) msg.textContent = '';
            await loadDiscussions();
        } catch (e) {
            if (msg) msg.textContent = '发送失败：' + e.message;
        } finally {
            if (btn) btn.disabled = false;
        }
    }

    // ============================================================
    // 初始化
    // ============================================================

    function init() {
        // 加载话题信息
        loadTopic().catch(function (e) {
            setText('topicTitle', '加载失败');
            setText('topicMeta', e.message);
            return;
        });

        // 文章列表
        if (qs('topicArticles')) loadArticles();

        // 讨论区
        if (qs('topicDiscussions')) loadDiscussions();

        // 发送按钮
        var sendBtn = qs('discussSendBtn');
        if (sendBtn) {
            sendBtn.addEventListener('click', submitDiscussion);
        }

        // 输入框事件
        var input = qs('topicDiscussionInput');
        if (input) {
            input.addEventListener('keydown', function (e) {
                if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
                    e.preventDefault();
                    submitDiscussion();
                }
            });
            input.addEventListener('input', function () {
                this.style.height = 'auto';
                this.style.height = Math.min(this.scrollHeight, 96) + 'px';
            });
        }
    }

    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
    else init();
})();
