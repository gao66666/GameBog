(function () {
    const { qs, api, getAuth } = window.GoBlog;

    const CY_MARK = '👁 ';
    const CY_MAX_LEN = 200;
    const COMMENT_MAX_LEN = 500;

    let currentViewCount = 0;
    let currentLikeCount = 0;
    let currentCommentCount = 0;
    let currentArticleData = null;
    let currentParentId = '0';
    let pendingCY = false;
    let cyFloatTimer = null;
    let isCollected = false;

    function pageEl() {
        return document.getElementById('page');
    }

    function getArticleId() {
        const el = pageEl();
        return (el && el.getAttribute('data-article-id')) || '';
    }

    function setActionHint(text) {
        const el = qs('actionHint');
        if (el) el.textContent = text || '';
    }

    function updateLikeBadge() {
        const el = qs('likeCount');
        if (el) el.textContent = String(currentLikeCount);
    }

    function renderMeta(d) {
        if (!d) return;
        const view = d && (d.view_count ?? d.viewCount ?? currentViewCount ?? 0);
        const like = d && (d.like_count ?? d.likeCount ?? currentLikeCount ?? 0);
        currentViewCount = Number(view) || 0;
        currentLikeCount = Number(like) || 0;
        updateLikeBadge();

        const created = d && (d.created_at || d.createdAt || '');
        const metaEl = qs('articleMeta');
        if (!metaEl) return;

        const commentPart = currentCommentCount > 0
            ? '<span class="stat"><span class="stat-icon" aria-hidden="true">💬</span>' + currentCommentCount + '</span>'
            : '';

        metaEl.innerHTML = [
            '<span class="stat"><span class="stat-icon" aria-hidden="true">阅读</span>' + currentViewCount + '</span>',
            '<span class="stat"><span class="stat-icon" aria-hidden="true">👍</span>' + currentLikeCount + '</span>',
            commentPart,
            created ? '<span class="stat">' + escapeHtml(created) + '</span>' : ''
        ].filter(Boolean).join('');

        const cc = qs('commentCount');
        if (cc) {
            cc.textContent = currentCommentCount > 0 ? '· ' + currentCommentCount + ' 条' : '';
        }
    }

    function renderTags(d) {
        const root = qs('articleTags');
        if (!root) return;

        const tags = (d && (d.tags || d.Tags)) || [];
        if (!Array.isArray(tags) || tags.length === 0) {
            root.innerHTML = '';
            root.style.display = 'none';
            return;
        }

        root.innerHTML = '';
        root.style.display = '';

        tags.forEach((t) => {
            const name = String((t && (t.name || t.Name)) || '').trim();
            if (!name) return;
            const chip = document.createElement('span');
            chip.className = 'tag';
            chip.textContent = '#' + name;
            root.appendChild(chip);
        });

        if (!root.childNodes.length) {
            root.style.display = 'none';
        }
    }

    function setText(id, text) {
        const el = qs(id);
        if (el) el.textContent = text;
    }

    function normalizeGithubURL(raw) {
        const trimmed = String(raw || '').trim();
        if (!trimmed) return '';

        let candidate = trimmed;
        if (!/^https?:\/\//i.test(candidate)) {
            if (/^(www\.)?github\.com\//i.test(candidate)) {
                candidate = 'https://' + candidate;
            } else if (/^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38})$/.test(candidate)) {
                candidate = 'https://github.com/' + candidate;
            } else {
                candidate = 'https://' + candidate;
            }
        }

        try {
            const u = new URL(candidate);
            if (!/^https?:$/i.test(u.protocol)) return '';
            return u.href;
        } catch {
            return '';
        }
    }

    function bindGithubLink(el, raw) {
        if (!el) return;
        const url = normalizeGithubURL(raw);
        if (url) {
            el.href = url;
            el.target = '_blank';
            el.rel = 'noopener noreferrer';
            el.onclick = null;
            return;
        }

        el.href = '#';
        el.removeAttribute('target');
        el.removeAttribute('rel');
        el.onclick = function (e) {
            e.preventDefault();
            alert('该用户尚未设置 GitHub 链接');
            return false;
        };
    }

    function applyHighlight(root) {
        if (!root) return;
        if (!window.hljs || typeof window.hljs.highlightElement !== 'function') return;
        const blocks = root.querySelectorAll('pre code');
        if (!blocks || !blocks.length) return;
        blocks.forEach((codeEl) => {
            try { window.hljs.highlightElement(codeEl); } catch { /* ignore */ }
        });
    }

    function escapeHtml(str) {
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    }

    function hasMarked() {
        const m = window.marked;
        return !!(m && typeof m.parse === 'function' && typeof m.setOptions === 'function');
    }

    function renderMarkdown(text) {
        const m = window.marked;
        if (!hasMarked() || !m || typeof m.Renderer !== 'function') {
            return '';
        }

        const renderer = new m.Renderer();

        renderer.html = function (html) {
            if (html && typeof html === 'object') {
                return escapeHtml(html.text || '');
            }
            return escapeHtml(html);
        };

        renderer.code = function (code, infostring) {
            let codeText = '';
            let lang = '';
            if (code && typeof code === 'object') {
                codeText = String(code.text || '');
                lang = String(code.lang || code.language || '');
            } else {
                codeText = String(code || '');
                lang = String((infostring || '').trim().split(/\s+/)[0] || '');
            }
            const cls = lang ? ('language-' + lang) : '';
            return '<pre><code' + (cls ? (' class="' + cls + '"') : '') + '>' + escapeHtml(codeText) + '</code></pre>';
        };

        try {
            if (!window.__goblog_marked_inited) {
                m.setOptions({
                    gfm: true,
                    breaks: true,
                    headerIds: false,
                    mangle: false,
                    renderer,
                });
                window.__goblog_marked_inited = true;
            } else {
                m.setOptions({ renderer });
            }
            return m.parse(String(text || ''));
        } catch {
            return '';
        }
    }

    function tocCardEl() { return qs('articleTocCard'); }
    function tocEl() { return qs('articleToc'); }

    function slugifyHeadingText(text) {
        let s = String(text || '').trim().toLowerCase();
        if (!s) return '';
        s = s.replace(/\s+/g, '-');
        s = s.replace(/[^\w\u4e00-\u9fa5\-]+/g, '');
        s = s.replace(/\-+/g, '-');
        s = s.replace(/^\-+|\-+$/g, '');
        return s;
    }

    function buildTOCFromContent(contentRoot) {
        const tocCard = tocCardEl();
        const tocRoot = tocEl();
        if (!tocCard || !tocRoot || !contentRoot) return;

        tocRoot.innerHTML = '';

        const headings = Array.from(contentRoot.querySelectorAll('h1,h2,h3,h4,h5,h6'));
        if (!headings.length) {
            tocCard.style.display = 'none';
            return;
        }

        const used = Object.create(null);
        headings.forEach((h, idx) => {
            const level = Number(String(h.tagName || '').replace(/^H/i, '')) || 1;
            let text = (h.textContent || '').trim();
            if (!text) text = 'section-' + (idx + 1);

            let id = String(h.getAttribute('id') || '').trim();
            if (!id) {
                id = slugifyHeadingText(text);
            }
            if (!id) {
                id = 'h-' + (idx + 1);
            }

            const base = id;
            let n = used[base] || 0;
            while (document.getElementById(id)) {
                n += 1;
                id = base + '-' + n;
            }
            used[base] = n;

            h.setAttribute('id', id);

            const a = document.createElement('a');
            a.className = 'article-toc-item level-' + level;
            a.href = '#' + id;
            a.textContent = text;
            tocRoot.appendChild(a);
        });

        tocCard.style.display = '';
    }

    function setReplyTarget(parentId, userName) {
        currentParentId = String(parentId || '0');
        const hint = qs('replyHint');
        if (hint) {
            if (currentParentId === '0') {
                hint.innerHTML = '';
            } else {
                hint.innerHTML = '回复 <strong>' + escapeHtml(userName || '') + '</strong> · <button type="button" id="cancelReplyBtn">取消</button>';
                const cancel = qs('cancelReplyBtn');
                if (cancel) {
                    cancel.onclick = function () {
                        setReplyTarget('0', '');
                    };
                }
            }
        }

        if (currentParentId !== '0') {
            pendingCY = false;
            syncCYUI();
        }

        const input = qs('commentInput');
        if (input) input.focus();
    }

    function syncCYUI() {
        const hint = qs('cyModeHint');
        if (hint) {
            hint.style.display = pendingCY ? '' : 'none';
        }
        const host = qs('commentEditorHost');
        const { token } = getAuth();
        const canCY = !!(token && currentParentId === '0');
        if (host) {
            host.classList.toggle('cy-disabled', !canCY);
        }
        const btn = qs('cyInsertBtn');
        if (btn) {
            btn.disabled = !canCY;
        }
    }

    function showCyFloat(show) {
        const host = qs('commentEditorHost');
        if (!host) return;
        if (show) {
            host.classList.add('cy-float-visible');
        } else {
            host.classList.remove('cy-float-visible');
        }
    }

    function scheduleHideCyFloat() {
        if (cyFloatTimer) clearTimeout(cyFloatTimer);
        cyFloatTimer = setTimeout(function () {
            const input = qs('commentInput');
            const active = document.activeElement;
            if (active !== input) {
                showCyFloat(false);
            }
        }, 220);
    }

    function fmtTime(iso) {
        try {
            const d = new Date(iso);
            if (isNaN(d.getTime())) return '';
            return d.toLocaleString();
        } catch { return ''; }
    }

    function updateCharCount() {
        const input = qs('commentInput');
        const el = qs('commentCharCount');
        if (!input || !el) return;
        const n = (input.value || '').length;
        const max = pendingCY ? CY_MAX_LEN : COMMENT_MAX_LEN;
        el.textContent = n + ' / ' + max;
        el.style.color = n > max ? '#c00' : '';
    }

    function insertAtCursor(textarea, text) {
        const start = textarea.selectionStart || 0;
        const end = textarea.selectionEnd || 0;
        const v = textarea.value || '';
        textarea.value = v.slice(0, start) + text + v.slice(end);
        const pos = start + text.length;
        textarea.selectionStart = textarea.selectionEnd = pos;
    }

    function renderCommentTree(list) {
        const root = qs('commentList');
        const empty = qs('commentEmpty');
        if (!root) return;

        root.innerHTML = '';

        const auth = getAuth();
        const currentUserId = auth && auth.userId ? String(auth.userId) : '';
        const hasLogin = auth && auth.token;

        if (!list || !list.length) {
            if (empty) {
                empty.textContent = '暂无评论，来抢沙发吧';
                empty.style.display = '';
            }
            return;
        }
        if (empty) empty.style.display = 'none';

        list.forEach((c, index) => {
            root.appendChild(buildCommentNode(c, index + 1, hasLogin, currentUserId, false, ''));
        });
    }

    function buildCommentNode(c, floorNo, hasLogin, currentUserId, isChild, replyToName) {
        const ctype = String((c.commentType || c.comment_type || 'comment')).toLowerCase();
        const isCY = ctype === 'cy';

        const box = document.createElement('div');
        box.className = 'comment-item' + (isCY ? ' comment-item--cy' : '');
        if (isChild) {
            box.classList.add('comment-child');
        }

        const header = document.createElement('div');
        header.className = 'comment-header';

        const av = document.createElement('div');
        av.className = 'comment-avatar';
        const uname = (c.user && c.user.userName) ? c.user.userName : '匿';
        av.textContent = uname.slice(0, 1);

        const nameWrap = document.createElement('div');
        nameWrap.style.display = 'flex';
        nameWrap.style.alignItems = 'center';
        nameWrap.style.gap = '8px';
        nameWrap.style.flexWrap = 'wrap';

        const userSpan = document.createElement('span');
        userSpan.className = 'comment-user';
        const baseName = (c.user && c.user.userName) ? c.user.userName : '匿名';
        if (isChild && replyToName) {
            userSpan.textContent = baseName + ' → ' + replyToName;
        } else {
            userSpan.textContent = baseName;
        }

        nameWrap.appendChild(userSpan);

        if (isCY) {
            const badge = document.createElement('span');
            badge.className = 'comment-badge';
            badge.textContent = 'CY';
            nameWrap.appendChild(badge);
        }

        const replyBtn = document.createElement('button');
        replyBtn.type = 'button';
        replyBtn.textContent = '回复';
        replyBtn.style.cssText = 'background:none;border:none;font-size:12px;color:#0071e3;cursor:pointer;padding:0;margin-left:4px;';
        replyBtn.onclick = function () {
            setReplyTarget(c.id, baseName);
        };

        const commentOwnerId = c && c.user && c.user.userId ? String(c.user.userId) : '';
        if (hasLogin && currentUserId && commentOwnerId && currentUserId === commentOwnerId) {
            const delBtn = document.createElement('button');
            delBtn.type = 'button';
            delBtn.textContent = '删除';
            delBtn.style.cssText = 'background:none;border:none;font-size:12px;color:#86868b;cursor:pointer;padding:0;margin-left:8px;';
            delBtn.onclick = async function () {
                if (!confirm('确定删除这条评论吗？')) return;
                const msg = qs('commentMsg');
                if (msg) msg.textContent = '';
                try {
                    await api('/api/v1/articles/comments/' + encodeURIComponent(String(c.id)), { method: 'DELETE' });
                    if (msg) msg.textContent = '已删除';
                    await loadComments();
                } catch (e) {
                    if (msg) msg.textContent = '删除失败：' + e.message;
                }
            };
            nameWrap.appendChild(delBtn);
        }

        nameWrap.appendChild(replyBtn);

        const time = document.createElement('span');
        time.className = 'comment-time';
        const createdRaw = c.createdAt || c.created_at || '';
        time.textContent = fmtTime(createdRaw) || createdRaw || (floorNo && !isChild ? floorNo + ' 楼' : '');

        header.appendChild(av);
        header.appendChild(nameWrap);
        header.appendChild(time);

        const contentEl = document.createElement('div');
        contentEl.className = 'comment-content';
        contentEl.textContent = c.content || '';

        box.appendChild(header);
        box.appendChild(contentEl);

        if (c.children && c.children.length) {
            const wrap = document.createElement('div');
            wrap.className = 'comment-children';
            c.children.forEach((child) => {
                const ru = child.replyUser && child.replyUser.userName ? child.replyUser.userName : '';
                wrap.appendChild(buildCommentNode(child, 0, hasLogin, currentUserId, true, ru || ''));
            });
            box.appendChild(wrap);
        }

        return box;
    }

    async function loadArticle() {
        const id = getArticleId();
        const resp = await api('/api/v1/articles/' + encodeURIComponent(id));
        const d = resp && resp.data;
        currentArticleData = d;

        isCollected = !!(d && (d.is_collected === true || d.isCollected === true));

        setText('articleTitle', d.title || ('文章 ' + id));
        renderMeta(d);
        renderTags(d);
        const coverEl = qs('articleCoverWrap');
        const coverImg = qs('articleCoverImg');
        const cu = String((d && (d.cover_url || d.coverUrl)) || '').trim();
        if (coverEl && coverImg && cu) {
            coverImg.src = cu;
            coverImg.alt = (d.title || '封面') + '';
            coverEl.hidden = false;
        } else if (coverEl) {
            coverEl.hidden = true;
        }
        const contentEl = qs('articleContent');
        if (contentEl) {
            const md = d.content || '';
            const html = renderMarkdown(md);
            if (html) {
                contentEl.innerHTML = html;
            } else if (d.html_content) {
                contentEl.innerHTML = d.html_content;
            } else {
                contentEl.textContent = md;
            }
            buildTOCFromContent(contentEl);
            applyHighlight(contentEl);
        }

        try {
            await loadAuthor(d.author_id || d.authorId);
        } catch (e) {
            setText('authorExtra', '作者信息加载失败');
        }

        setupLikeButton(d);
        setupCollectButton();
        updateCollectButton();
    }

    function setupLikeButton(articleData) {
        const btn = qs('likeBtn');
        if (!btn) return;

        const { token } = getAuth();
        setActionHint('');

        if (!token) {
            btn.disabled = true;
            btn.title = '登录后可点赞';
            return;
        }

        btn.disabled = false;
        btn.title = '点赞';
        btn.onclick = async function () {
            const articleId = getArticleId();
            if (!articleId) return;

            btn.disabled = true;
            setActionHint('');

            const prevLike = currentLikeCount;
            currentLikeCount = prevLike + 1;
            renderMeta(Object.assign({}, articleData, { like_count: currentLikeCount, view_count: currentViewCount }));

            try {
                await api('/api/v1/articles/like', {
                    method: 'POST',
                    body: JSON.stringify({ article_id: String(articleId), is_cancel: false })
                });
                setActionHint('已点赞');
            } catch (e) {
                currentLikeCount = prevLike;
                renderMeta(Object.assign({}, articleData, { like_count: currentLikeCount, view_count: currentViewCount }));
                setActionHint('点赞失败：' + e.message);
            } finally {
                btn.disabled = false;
            }
        };
    }

    function updateCollectButton() {
        const btn = qs('collectBtn');
        if (!btn) return;
        btn.classList.toggle('active', isCollected);
        btn.title = isCollected ? '已收藏，点击取消' : '收藏';
    }

    function setupCollectButton() {
        const btn = qs('collectBtn');
        if (!btn) return;

        const { token } = getAuth();
        if (!token) {
            btn.disabled = true;
            btn.title = '登录后可收藏';
            return;
        }

        btn.disabled = false;
        btn.onclick = async function () {
            const articleId = getArticleId();
            if (!articleId) return;
            btn.disabled = true;
            try {
                if (isCollected) {
                    await api('/api/v1/articles/collect', {
                        method: 'DELETE',
                        body: JSON.stringify({ articleId: String(articleId) })
                    });
                    isCollected = false;
                    setActionHint('已取消收藏');
                } else {
                    await api('/api/v1/articles/collect', {
                        method: 'POST',
                        body: JSON.stringify({ articleId: String(articleId) })
                    });
                    isCollected = true;
                    setActionHint('已加入收藏');
                }
                updateCollectButton();
            } catch (e) {
                setActionHint('收藏操作失败：' + e.message);
            } finally {
                btn.disabled = false;
            }
        };
    }

    async function loadAuthor(authorId) {
        const avatarEl = qs('authorAvatar');
        const nameEl = qs('authorName');
        const extraEl = qs('authorExtra');

        if (!authorId) {
            if (avatarEl) { avatarEl.textContent = '?'; avatarEl.innerHTML = '?'; }
            if (nameEl) nameEl.textContent = '-';
            if (extraEl) extraEl.textContent = '';
            const home = qs('sideHomeLink');
            const github = qs('sideGithubLink');
            const mail = qs('sideMailLink');
            if (home) home.href = '#';
            bindGithubLink(github, '');
            if (mail) mail.href = '#';
            return;
        }

        const resp = await api('/api/v1/users/' + encodeURIComponent(authorId));
        const u = resp && resp.data;
        const name = (u && (u.user_name || u.userName)) || '-';
        const uid = String((u && (u.user_id || u.userId)) || authorId);

        if (nameEl) {
            nameEl.innerHTML = '<a href="/u/' + encodeURIComponent(uid) + '">' + escapeHtml(name) + '</a>';
        }
        if (extraEl) {
            extraEl.textContent = u && u.tel ? '用户 · 已验证' : '作者';
        }

        if (avatarEl) {
            const av = u && (u.avatar || u.Avatar);
            if (av && String(av).trim()) {
                avatarEl.innerHTML = '<img src="' + escapeHtml(String(av).trim()) + '" alt="" loading="lazy" referrerpolicy="no-referrer" />';
            } else {
                avatarEl.textContent = name.slice(0, 1) || '?';
            }
        }

        setText('sideAuthorName', name);
        const home = qs('sideHomeLink');
        const github = qs('sideGithubLink');
        const mail = qs('sideMailLink');
        if (home) home.href = '/u/' + encodeURIComponent(uid);
        bindGithubLink(github, u && (u.github || u.github_url || u.githubUrl));
        if (mail) mail.href = '/dm?to=' + encodeURIComponent(uid);

        setupFollowButton(uid);
    }

    function setupFollowButton(followingId) {
        const btn = qs('followBtn');
        if (!btn) return;

        const { token, userId } = getAuth();
        if (!token) {
            btn.style.display = 'none';
            return;
        }
        if (userId && String(userId) === String(followingId)) {
            btn.style.display = 'none';
            return;
        }

        btn.style.display = '';
        btn.disabled = false;
        btn.innerHTML = '<span aria-hidden="true">＋</span>';
        btn.title = '关注作者';
        btn.onclick = async function () {
            btn.disabled = true;
            try {
                await api('/api/v1/follow', {
                    method: 'POST',
                    body: JSON.stringify({ followingId: String(followingId) })
                });
                setActionHint('已关注作者');
                btn.style.display = 'none';
            } catch (e) {
                setActionHint('关注失败：' + e.message);
                btn.disabled = false;
            }
        };
    }

    async function loadComments() {
        const id = getArticleId();
        const resp = await api('/api/v1/articles/comments?article_id=' + encodeURIComponent(id) + '&page=1&size=50&limit=5');
        const data = resp && resp.data;
        const list = data && (data.list || data) || [];
        renderCommentTree(list);
        const total = data && (typeof data.total === 'number' ? data.total : list.length);
        currentCommentCount = total || 0;
        if (currentArticleData) {
            renderMeta(currentArticleData);
        }
    }

    async function submitComment() {
        const id = getArticleId();
        const { token, userId } = getAuth();
        const msg = qs('commentMsg');
        const input = qs('commentInput');
        if (msg) msg.textContent = '';

        if (!token) {
            if (msg) msg.textContent = '请先登录再发表评论';
            return;
        }

        const content = String((input && input.value) || '').trim();
        if (!content) {
            if (msg) msg.textContent = '评论不能为空';
            return;
        }

        const maxLen = (pendingCY && currentParentId === '0') ? CY_MAX_LEN : COMMENT_MAX_LEN;
        if (content.length > maxLen) {
            if (msg) msg.textContent = '内容过长（最多 ' + maxLen + ' 字）';
            return;
        }

        const submitBtn = qs('commentSubmitBtn');
        if (submitBtn) submitBtn.disabled = true;

        try {
            if (pendingCY && currentParentId === '0') {
                await api('/api/v1/articles/comments/cy', {
                    method: 'POST',
                    body: JSON.stringify({
                        articleId: String(id),
                        content
                    })
                });
                pendingCY = false;
                syncCYUI();
            } else {
                await api('/api/v1/articles/comments', {
                    method: 'POST',
                    body: JSON.stringify({
                        articleId: String(id),
                        userId: String(userId || '0'),
                        parentId: String(currentParentId || '0'),
                        content
                    })
                });
            }

            if (input) input.value = '';
            setReplyTarget('0', '');
            if (msg) msg.textContent = '已发送';
            updateCharCount();
            await loadComments();
        } catch (e) {
            if (msg) msg.textContent = '发送失败：' + e.message;
        } finally {
            if (submitBtn) submitBtn.disabled = false;
        }
    }

    function wireCommentComposer() {
        const input = qs('commentInput');
        const submitBtn = qs('commentSubmitBtn');
        const cyBtn = qs('cyInsertBtn');
        const host = qs('commentEditorHost');

        if (submitBtn) {
            submitBtn.addEventListener('click', function (e) {
                e.preventDefault();
                submitComment();
            });
        }

        if (input) {
            input.addEventListener('input', function () {
                if (pendingCY && !(input.value || '').startsWith('👁')) {
                    pendingCY = false;
                    syncCYUI();
                }
                updateCharCount();
            });
            input.addEventListener('focus', function () {
                if (cyFloatTimer) clearTimeout(cyFloatTimer);
                const { token } = getAuth();
                if (token && currentParentId === '0') {
                    showCyFloat(true);
                } else {
                    showCyFloat(false);
                }
            });
            input.addEventListener('blur', function () {
                scheduleHideCyFloat();
            });
            input.addEventListener('keydown', function (e) {
                if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                    e.preventDefault();
                    submitComment();
                }
            });
        }

        if (cyBtn) {
            cyBtn.addEventListener('mousedown', function (e) {
                e.preventDefault();
            });
            cyBtn.addEventListener('click', function () {
                if (!input || cyBtn.disabled) return;
                insertAtCursor(input, CY_MARK);
                pendingCY = true;
                syncCYUI();
                updateCharCount();
                input.focus();
            });
        }

        if (host) {
            host.addEventListener('mousedown', function () {
                if (cyFloatTimer) clearTimeout(cyFloatTimer);
            });
        }

        syncCYUI();
        updateCharCount();
    }

    document.addEventListener('DOMContentLoaded', async function () {
        wireCommentComposer();

        try {
            await loadArticle();
        } catch (e) {
            setText('articleTitle', '加载失败');
            const c = qs('articleContent');
            if (c) c.textContent = e.message || String(e);
        }

        try {
            await loadComments();
        } catch (e) {
            const root = qs('commentList');
            const empty = qs('commentEmpty');
            if (root) root.innerHTML = '';
            if (empty) {
                empty.style.display = '';
                empty.textContent = '评论加载失败：' + (e.message || String(e));
            }
        }
    });
})();
