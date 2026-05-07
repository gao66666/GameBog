(function () {
    'use strict';

    if (!window.GoBlog) return;

    var qs = window.GoBlog.qs;
    var getAuth = window.GoBlog.getAuth;
    var api = window.GoBlog.api;
    var ensureWSConnected = window.GoBlog.ensureWSConnected;
    var getWSStatus = window.GoBlog.getWSStatus;
    var setAuth = window.GoBlog.setAuth;
    var clearAuth = window.GoBlog.clearAuth;

    var DM_HINT_KEY = 'gb_dm_hint';
    var DEFAULT_ARTICLE_COVER = 'https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg';
    function pickArticleCoverUrl(a) {
        var u = String((a && (a.coverUrl || a.cover_url)) || '').trim();
        return /^https?:\/\//i.test(u) ? u : DEFAULT_ARTICLE_COVER;
    }

    var state = {
        page: 1,
        size: 8,
        total: 0,
        loadSeq: 0,
        currentGithub: '',
        cyLoaded: false
    };

    function setText(id, text) {
        var el = qs(id);
        if (el) el.textContent = text;
    }

    function setHTML(id, html) {
        var el = qs(id);
        if (el) el.innerHTML = html;
    }

    function setVisible(id, show) {
        var el = qs(id);
        if (el) el.style.display = show ? '' : 'none';
    }

    function pick(obj, keys, fallback) {
        for (var i = 0; i < keys.length; i += 1) {
            var k = keys[i];
            if (obj && obj[k] !== undefined && obj[k] !== null) return obj[k];
        }
        return fallback;
    }

    function firstChar(text) {
        var t = String(text || '').trim();
        return t ? t.charAt(0).toUpperCase() : 'G';
    }

    function normalizeGithubURL(raw) {
        var trimmed = String(raw || '').trim();
        if (!trimmed) return '';

        var candidate = trimmed;
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
            var u = new URL(candidate);
            if (!/^https?:$/i.test(u.protocol)) return '';
            return u.href;
        } catch (e) {
            return '';
        }
    }

    function setDmDot(on) {
        var dot = qs('dmDot');
        if (dot) dot.style.display = on ? 'inline-block' : 'none';
    }

    function setDmHint(on) {
        try {
            if (on) localStorage.setItem(DM_HINT_KEY, '1');
            else localStorage.removeItem(DM_HINT_KEY);
        } catch (e) { /* ignore */ }
        setDmDot(!!on);
    }

    function hasDmHint() {
        try {
            return localStorage.getItem(DM_HINT_KEY) === '1';
        } catch (e) {
            return false;
        }
    }

    function setWsStatus(text) {
        var dot = qs('wsDot');
        var label = qs('wsText');
        if (label) label.textContent = text;
        if (dot) {
            if (text === '已连接') dot.classList.add('on');
            else dot.classList.remove('on');
        }
    }

    function setupWS() {
        setWsStatus(getWSStatus ? (getWSStatus() || '未连接') : '未连接');

        window.addEventListener('goblog:ws-status', function (ev) {
            var s = ev && ev.detail && ev.detail.status;
            if (s) setWsStatus(s);
        });

        window.addEventListener('goblog:ws-message', function (ev) {
            var data = ev && ev.detail && ev.detail.data;
            if (!data) return;

            var obj = null;
            try { obj = JSON.parse(data); } catch (e) { return; }

            if (obj && (obj.type === 'dm' || obj.type === 'dm_unread')) {
                setDmHint(true);
            }

            if (isNotificationLike(obj)) {
                addNotification(obj);
            }
        });

        try { ensureWSConnected(); } catch (e) { /* ignore */ }
    }

    function isNotificationLike(obj) {
        if (!obj || typeof obj !== 'object') return false;
        return ('event_id' in obj) || ('eventId' in obj) || ('sender_id' in obj) ||
            ('senderId' in obj) || ('sender_name' in obj) || ('senderName' in obj);
    }

    function addNotification(item) {
        var list = qs('notifList');
        var empty = qs('notifEmpty');
        var badge = qs('notifBadge');
        if (!list) return;

        if (empty) empty.style.display = 'none';
        if (badge) badge.style.display = 'inline-block';

        var div = document.createElement('div');
        div.className = 'notif-item';

        var content = pick(item, ['content', 'Content'], '有一条新通知');
        var sender = pick(item, ['senderName', 'sender_name'], '');
        var type = pick(item, ['type'], '');

        var line1 = document.createElement('div');
        line1.textContent = content;
        div.appendChild(line1);

        var meta = document.createElement('div');
        meta.className = 'muted';
        meta.textContent = (sender ? ('来自 ' + sender + ' · ') : '') + (type ? ('类型 ' + type) : '');
        div.appendChild(meta);

        list.insertBefore(div, list.firstChild);
    }

    async function loadMeBase() {
        var uidEl = qs('meUid');
        var nameEl = qs('meName');
        var emailEl = qs('meEmail');
        var avatarEl = qs('meAvatar');

        var auth = getAuth();
        if (uidEl) uidEl.textContent = auth.userId ? ('UID: ' + auth.userId) : 'UID: -';
        if (nameEl) nameEl.textContent = auth.userName || '-';
        if (emailEl) emailEl.textContent = '';
        if (avatarEl) avatarEl.textContent = firstChar(auth.userName);

        if (!auth.token) {
            setText('statBalance', '—');
            return;
        }

        try {
            var resp = await api('/api/v1/users/me');
            var d = resp && resp.data;
            var uid = pick(d, ['user_id', 'userId'], auth.userId);
            var uname = pick(d, ['user_name', 'userName'], auth.userName);
            var email = pick(d, ['email', 'Email'], '');
            var avatar = pick(d, ['avatar'], '');
            state.currentGithub = String(pick(d, ['github', 'github_url', 'githubUrl'], '')).trim();

            if (uidEl) uidEl.textContent = uid ? ('UID: ' + uid) : 'UID: -';
            if (nameEl) nameEl.textContent = uname || '-';
            if (emailEl) emailEl.textContent = email || '';
            if (avatarEl) avatarEl.textContent = firstChar(uname || avatar);

            var balRaw = pick(d, ['account_balance', 'accountBalance'], null);
            if (balRaw !== null && balRaw !== undefined && String(balRaw).trim() !== '') {
                var bn = Number(balRaw);
                setText('statBalance', Number.isFinite(bn) ? String(bn) : String(balRaw));
            } else {
                setText('statBalance', '—');
            }

            var githubInput = qs('setGithub');
            if (githubInput) githubInput.value = state.currentGithub;

            if (uname && auth && auth.token) {
                setAuth({ token: auth.token, userId: String(uid || auth.userId || ''), userName: String(uname) });
            }
        } catch (e) {
            setText('statBalance', '—');
        }
    }

    async function loadDMUnread() {
        var auth = getAuth();
        if (!auth.token) return;

        var hinted = hasDmHint();
        if (hinted) setDmDot(true);

        try {
            var r = await api('/api/v1/dm/unread_count');
            var d = r && r.data;
            var cnt = Number(pick(d, ['count'], 0)) || 0;
            if (cnt > 0) {
                setDmHint(true);
                return;
            }
            setDmDot(hinted);
        } catch (e) {
            setDmDot(hinted);
        }
    }

    function setupSettings() {
        var btn = qs('settingsBtn');
        var panel = qs('settingsPanel');
        var cancelBtn = qs('settingsCancel');
        var saveBtn = qs('settingsSave');
        var msg = qs('settingsMsg');

        function show(on) {
            if (!panel) return;
            panel.style.display = on ? '' : 'none';
            if (!on) {
                var name = qs('setName');
                var email = qs('setEmail');
                var pwd = qs('setPwd');
                var github = qs('setGithub');
                if (name) name.value = '';
                if (email) email.value = '';
                if (pwd) pwd.value = '';
                if (github) github.value = state.currentGithub;
                if (msg) msg.textContent = '';
            } else {
                var gh = qs('setGithub');
                if (gh) gh.value = state.currentGithub;
            }
        }

        if (btn) btn.addEventListener('click', function () { show(true); });
        if (cancelBtn) cancelBtn.addEventListener('click', function () { show(false); });

        if (saveBtn) {
            saveBtn.addEventListener('click', async function () {
                var auth = getAuth();
                if (!auth.token) {
                    if (msg) msg.textContent = '未登录';
                    return;
                }

                var name = qs('setName');
                var email = qs('setEmail');
                var pwd = qs('setPwd');
                var github = qs('setGithub');

                var payload = {};
                var nameVal = name && String(name.value || '').trim();
                var emailVal = email && String(email.value || '').trim();
                var pwdVal = pwd && String(pwd.value || '').trim();
                var ghVal = github && String(github.value || '').trim();

                if (nameVal) payload.name = nameVal;
                if (emailVal) payload.email = emailVal;
                if (pwdVal) payload.password = pwdVal;
                if (github) {
                    if (ghVal) {
                        var normalized = normalizeGithubURL(ghVal);
                        if (!normalized) {
                            if (msg) msg.textContent = 'GitHub 链接格式不正确';
                            return;
                        }
                        if (normalized !== state.currentGithub) payload.github = normalized;
                    } else if (state.currentGithub) {
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

    async function loadMyArticles(userId) {
        var seq = ++state.loadSeq;
        var root = qs('articleList');
        var empty = qs('articleEmpty');
        var info = qs('artPageInfo');
        var prevBtn = qs('artPrev');
        var nextBtn = qs('artNext');

        if (root) root.innerHTML = '';
        if (empty) empty.textContent = '加载中...';

        if (!userId) {
            if (empty) empty.textContent = '未登录：无法查看我的文章';
            if (prevBtn) prevBtn.disabled = true;
            if (nextBtn) nextBtn.disabled = true;
            return;
        }

        try {
            var url = '/api/v1/articles/list?author_id=' + encodeURIComponent(userId) +
                '&page=' + state.page + '&size=' + state.size;
            var resp = await api(url);
            if (seq !== state.loadSeq) return;

            var data = resp && resp.data;
            var list = pick(data, ['article_list', 'articles'], []);
            state.total = Number(pick(data, ['total'], 0)) || 0;

            setText('statArticles', String(state.total || 0));

            if (!list || list.length === 0) {
                if (empty) empty.textContent = '暂无文章';
                if (prevBtn) prevBtn.disabled = state.page <= 1;
                if (nextBtn) nextBtn.disabled = true;
                if (info) info.textContent = '';
                return;
            }

            if (empty) empty.textContent = '';

            list.forEach(function (a) {
                var item = document.createElement('div');
                item.className = 'me-item me-item-row';

                var thumb = document.createElement('img');
                thumb.className = 'me-item-thumb';
                thumb.src = pickArticleCoverUrl(a);
                thumb.alt = '';
                thumb.loading = 'lazy';
                item.appendChild(thumb);

                var body = document.createElement('div');
                body.className = 'me-item-body';

                var title = document.createElement('a');
                title.className = 'me-item-title';
                title.href = '/article/' + encodeURIComponent(a.id);
                title.textContent = a.title || ('文章 ' + a.id);
                body.appendChild(title);

                var summary = String(pick(a, ['summary', 'Summary'], '')).slice(0, 160);
                if (summary) {
                    var desc = document.createElement('div');
                    desc.className = 'me-item-desc';
                    desc.textContent = summary;
                    body.appendChild(desc);
                }

                var meta = document.createElement('div');
                meta.className = 'me-item-meta';
                var view = Number(pick(a, ['viewCount', 'view_count'], 0)) || 0;
                var like = Number(pick(a, ['likeCount', 'like_count'], 0)) || 0;
                var comment = Number(pick(a, ['commentCount', 'comment_count'], 0)) || 0;
                var created = pick(a, ['createdAt', 'created_at'], '');
                var parts = [];
                if (view) parts.push('阅读 ' + view);
                if (like) parts.push('点赞 ' + like);
                if (comment) parts.push('评论 ' + comment);
                if (created) parts.push(String(created).slice(0, 10));
                meta.textContent = parts.join(' · ');
                body.appendChild(meta);

                var actions = document.createElement('div');
                actions.className = 'me-item-actions';

                var editBtn = document.createElement('button');
                editBtn.className = 'btn-edit';
                editBtn.textContent = '编辑';
                editBtn.addEventListener('click', function () {
                    location.href = '/editor?id=' + encodeURIComponent(a.id);
                });

                var delBtn = document.createElement('button');
                delBtn.textContent = '删除';
                delBtn.className = 'btn-del';
                delBtn.addEventListener('click', async function () {
                    if (!confirm('确定删除这篇文章吗？')) return;
                    try {
                        await api('/api/v1/articles/' + encodeURIComponent(a.id), { method: 'DELETE' });
                        await loadMyArticles(userId);
                    } catch (e) {
                        alert('删除失败：' + (e && e.message || ''));
                    }
                });

                actions.appendChild(editBtn);
                actions.appendChild(delBtn);

                var mainCol = document.createElement('div');
                mainCol.className = 'me-item-main';
                mainCol.appendChild(body);
                mainCol.appendChild(actions);

                item.appendChild(mainCol);
                if (root) root.appendChild(item);
            });

            var maxPage = Math.max(1, Math.ceil(state.total / state.size));
            if (info) info.textContent = '第 ' + state.page + ' / ' + maxPage + ' 页';
            if (prevBtn) prevBtn.disabled = state.page <= 1;
            if (nextBtn) nextBtn.disabled = state.page >= maxPage;
        } catch (e) {
            if (empty) empty.textContent = '加载失败：' + (e && e.message || '');
            if (prevBtn) prevBtn.disabled = true;
            if (nextBtn) nextBtn.disabled = true;
        }
    }

    async function loadMyCollections(userId) {
        var root = qs('colList');
        var empty = qs('colEmpty');
        if (root) root.innerHTML = '';
        if (empty) empty.textContent = '加载中...';

        if (!userId) {
            if (empty) empty.textContent = '未登录';
            return;
        }

        try {
            var resp = await api('/api/v1/articles/collect');
            var data = resp && resp.data;
            var list = pick(data, ['list', 'article_list', 'collections'], []);

            if (!list || list.length === 0) {
                if (empty) empty.textContent = '暂无收藏';
                return;
            }

            if (empty) empty.textContent = '';

            list.forEach(function (c) {
                var item = document.createElement('div');
                item.className = 'me-item me-item-row';

                var thumb = document.createElement('img');
                thumb.className = 'me-item-thumb';
                thumb.src = pickArticleCoverUrl(c);
                thumb.alt = '';
                thumb.loading = 'lazy';
                item.appendChild(thumb);

                var body = document.createElement('div');
                body.className = 'me-item-body';

                var articleId = pick(c, ['articleId', 'article_id', 'id'], '');
                var title = String(pick(c, ['title'], '') || '').trim();
                if (!title) title = articleId ? '（无标题）' : '文章';
                var summary = String(pick(c, ['summary'], '') || '').trim();

                var link = document.createElement('a');
                link.className = 'me-item-title';
                link.href = '/article/' + encodeURIComponent(articleId);
                link.textContent = title;
                body.appendChild(link);

                if (summary) {
                    var desc = document.createElement('div');
                    desc.className = 'me-item-desc';
                    desc.textContent = summary.length > 220 ? summary.slice(0, 220) + '…' : summary;
                    body.appendChild(desc);
                }

                var tags = pick(c, ['tags'], []);
                if (Array.isArray(tags) && tags.length) {
                    var tagRow = document.createElement('div');
                    tagRow.className = 'me-item-tags';
                    tags.forEach(function (t) {
                        if (!t) return;
                        var name = String(t.name != null ? t.name : t.Name || '').trim();
                        if (!name) return;
                        var pill = document.createElement('span');
                        pill.className = 'me-tag';
                        pill.textContent = name;
                        tagRow.appendChild(pill);
                    });
                    if (tagRow.childNodes.length) body.appendChild(tagRow);
                }

                var meta = document.createElement('div');
                meta.className = 'me-item-meta';
                var collected = pick(c, ['collectedAt', 'collected_at', 'created_at', 'createdAt'], '');
                meta.textContent = collected ? ('收藏于 ' + String(collected).slice(0, 10)) : '已收藏';
                body.appendChild(meta);

                var mainCol = document.createElement('div');
                mainCol.className = 'me-item-main';
                mainCol.appendChild(body);

                item.appendChild(mainCol);
                if (root) root.appendChild(item);
            });
        } catch (e) {
            if (empty) empty.textContent = '加载失败：' + (e && e.message || '');
        }
    }

    function fmtTime(iso) {
        if (!iso) return '';
        try {
            var d = new Date(iso);
            if (isNaN(d.getTime())) return '';
            return d.toLocaleString();
        } catch (e) {
            return '';
        }
    }

    async function loadMyCYList(userId) {
        var root = qs('cyList');
        var empty = qs('cyEmpty');
        if (root) root.innerHTML = '';
        if (empty) empty.textContent = '加载中...';

        if (!userId) {
            if (empty) empty.textContent = '未登录';
            return;
        }

        try {
            var resp = await api('/api/v1/comments/cy');
            var data = resp && resp.data;
            var list = pick(data, ['list'], []);

            if (!list || list.length === 0) {
                if (empty) empty.textContent = '暂无插眼记录，去文章评论区试试吧';
                return;
            }

            if (empty) empty.textContent = '';

            list.forEach(function (row) {
                var card = document.createElement('div');
                card.className = 'me-cy-card';

                var accent = document.createElement('div');
                accent.className = 'me-cy-accent';
                card.appendChild(accent);

                var inner = document.createElement('div');
                inner.className = 'me-cy-inner';

                var top = document.createElement('div');
                top.className = 'me-cy-top';

                var badge = document.createElement('span');
                badge.className = 'me-cy-badge';
                badge.textContent = 'CY';
                top.appendChild(badge);

                var time = document.createElement('span');
                time.className = 'me-cy-time';
                time.textContent = fmtTime(pick(row, ['createdAt', 'created_at'], ''));
                top.appendChild(time);

                inner.appendChild(top);

                var aid = pick(row, ['articleId', 'article_id'], '');
                var title = pick(row, ['articleTitle', 'article_title'], '') || ('文章 ' + aid);
                var artRow = document.createElement('div');
                artRow.className = 'me-cy-article';
                var alink = document.createElement('a');
                alink.href = '/article/' + encodeURIComponent(String(aid));
                alink.textContent = title;
                artRow.appendChild(alink);
                inner.appendChild(artRow);

                var content = document.createElement('div');
                content.className = 'me-cy-content';
                content.textContent = pick(row, ['content'], '') || '';
                inner.appendChild(content);

                card.appendChild(inner);
                if (root) root.appendChild(card);
            });
        } catch (e) {
            if (empty) empty.textContent = '加载失败：' + (e && e.message || '');
        }
    }

    async function loadMyFollowedTopics(userId) {
        var root = qs('topicList');
        var empty = qs('topicEmpty');
        if (root) root.innerHTML = '';
        if (empty) empty.textContent = '加载中...';

        if (!userId) {
            if (empty) empty.textContent = '未登录';
            return;
        }

        try {
            var resp = await api('/api/v1/follow/topics');
            var data = resp && resp.data;
            var list = pick(data, ['list', 'topics', 'topic_list'], []);

            if (!list || list.length === 0) {
                if (empty) empty.textContent = '暂未关注任何话题';
                return;
            }

            if (empty) empty.textContent = '';

            list.forEach(function (t) {
                var item = document.createElement('div');
                item.className = 'me-item';

                var body = document.createElement('div');
                body.className = 'me-item-body';

                var topicId = pick(t, ['topic_id', 'topicId', 'id'], '');
                var name = pick(t, ['topicName', 'topic_name', 'name'], '') || ('话题 ' + topicId);

                var title = document.createElement('a');
                title.className = 'me-item-title';
                title.href = '/topic/' + encodeURIComponent(topicId);
                title.textContent = name;
                body.appendChild(title);

                var meta = document.createElement('div');
                meta.className = 'me-item-meta';
                var created = pick(t, ['created_at', 'createdAt'], '');
                meta.textContent = created ? ('关注于 ' + String(created).slice(0, 10)) : '已关注';
                body.appendChild(meta);

                item.appendChild(body);
                if (root) root.appendChild(item);
            });
        } catch (e) {
            if (empty) empty.textContent = '加载失败：' + (e && e.message || '');
        }
    }

    async function loadMyGamePlays(userId) {
        var root = qs('gameList');
        var empty = qs('gameEmpty');
        if (root) root.innerHTML = '';
        if (empty) empty.textContent = '加载中...';

        if (!userId) {
            if (empty) empty.textContent = '未登录';
            return;
        }

        try {
            var resp = await api('/api/v1/games/play');
            var data = resp && resp.data;
            var list = pick(data, ['list', 'plays', 'game_list'], []);

            if (!list || list.length === 0) {
                if (empty) empty.textContent = '暂无游戏记录';
                return;
            }

            if (empty) empty.textContent = '';

            list.forEach(function (g) {
                var item = document.createElement('div');
                item.className = 'me-item';

                var body = document.createElement('div');
                body.className = 'me-item-body';

                var gameName = pick(g, ['game_name', 'name'], '') ||
                    pick(g.game || {}, ['name'], '') ||
                    ('游戏 ' + pick(g, ['game_id', 'gameId'], ''));

                var nameEl = document.createElement('div');
                nameEl.className = 'game-name';
                nameEl.textContent = gameName;
                body.appendChild(nameEl);

                var status = pick(g, ['status'], '');
                var playtime = pick(g, ['playtimeTotal', 'playtime_total'], '');
                var stats = document.createElement('div');
                stats.className = 'game-stats';
                stats.textContent =
                    (status ? ('状态: ' + status + ' ') : '') +
                    (playtime ? ('· 总时长 ' + playtime + 'h') : '');
                body.appendChild(stats);

                item.appendChild(body);
                if (root) root.appendChild(item);
            });
        } catch (e) {
            if (empty) empty.textContent = '加载失败：' + (e && e.message || '');
        }
    }

    async function loadFollowingUserCount(userId) {
        if (!userId) {
            setText('statFollows', '0');
            return;
        }
        try {
            var resp = await api('/api/v1/follow/users/count');
            var d = resp && resp.data;
            var n = Number(pick(d, ['count'], 0));
            if (!Number.isFinite(n)) n = 0;
            setText('statFollows', String(n));
        } catch (e) {
            setText('statFollows', '0');
        }
    }

    async function loadFanCount(userId) {
        if (!userId) {
            setText('statFans', '0');
            return;
        }
        try {
            var resp = await api('/api/v1/users/' + encodeURIComponent(userId));
            var d = resp && resp.data;
            var cnt = pick(d, ['follower_count', 'followerCount'], 0);
            setText('statFans', String(cnt || 0));
        } catch (e) {
            setText('statFans', '0');
        }
    }

    async function loadMyPoints() {
        var auth = getAuth();
        if (!auth.token) {
            setText('statPoints', '—');
            return;
        }
        try {
            var resp = await api('/api/v1/points/wallet');
            var d = resp && resp.data;
            var bal = Number(pick(d, ['balance', 'Balance'], 0));
            if (!Number.isFinite(bal)) bal = 0;
            setText('statPoints', String(bal));
        } catch (e) {
            setText('statPoints', '—');
        }
    }

    function setupTabs() {
        var tabs = document.querySelectorAll('.me-tab');
        tabs.forEach(function (tab) {
            tab.addEventListener('click', function () {
                var tabName = this.getAttribute('data-tab');
                if (!tabName) return;

                tabs.forEach(function (t) { t.classList.remove('active'); });
                this.classList.add('active');

                document.querySelectorAll('.me-panel').forEach(function (p) {
                    p.classList.remove('active');
                });
                var panel = qs('panel-' + tabName);
                if (panel) panel.classList.add('active');

                if (tabName === 'cy' && !state.cyLoaded) {
                    state.cyLoaded = true;
                    loadMyCYList(getAuth().userId);
                }
            });
        });
    }

    function setupPager(userId) {
        var prevBtn = qs('artPrev');
        var nextBtn = qs('artNext');
        if (prevBtn) {
            prevBtn.addEventListener('click', function () {
                if (state.page <= 1) return;
                state.page -= 1;
                loadMyArticles(userId);
            });
        }
        if (nextBtn) {
            nextBtn.addEventListener('click', function () {
                var maxPage = Math.max(1, Math.ceil(state.total / state.size));
                if (state.page >= maxPage) return;
                state.page += 1;
                loadMyArticles(userId);
            });
        }
    }

    function setupBackTop() {
        var btn = qs('backTop');
        if (!btn) return;
        window.addEventListener('scroll', function () {
            if (window.pageYOffset > 300) btn.classList.add('show');
            else btn.classList.remove('show');
        });
        btn.addEventListener('click', function () {
            window.scrollTo({ top: 0, behavior: 'smooth' });
        });
    }

    function setupLogout() {
        var btn = qs('logoutBtn');
        if (!btn) return;
        btn.addEventListener('click', function () {
            clearAuth();
            location.href = '/login';
        });
    }

    function init() {
        var auth = getAuth();

        setDmDot(hasDmHint());
        setupTabs();
        setupSettings();
        setupPager(auth.userId);
        setupBackTop();
        setupLogout();

        loadMeBase();
        loadMyArticles(auth.userId);
        loadMyCollections(auth.userId);
        loadMyFollowedTopics(auth.userId);
        loadMyGamePlays(auth.userId);
        loadFollowingUserCount(auth.userId);
        loadFanCount(auth.userId);
        loadMyPoints();

        if (!auth.token) {
            setWsStatus('未登录');
            return;
        }

        setupWS();
        loadDMUnread();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
