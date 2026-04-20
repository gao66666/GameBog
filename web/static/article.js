(function () {
    const { qs, api, getAuth } = window.GoBlog;

    let currentViewCount = 0;
    let currentLikeCount = 0;
    let currentCommentCount = 0;
    let currentArticleData = null;

    function pageEl() { return document.getElementById('page'); }

    function getArticleId() {
        const el = pageEl();
        return (el && el.getAttribute('data-article-id')) || '';
    }

    function renderMeta(d, id) {
        if (!d) return;
        const view = d && (d.view_count ?? d.viewCount ?? currentViewCount ?? 0);
        const like = d && (d.like_count ?? d.likeCount ?? currentLikeCount ?? 0);
        currentViewCount = Number(view) || 0;
        currentLikeCount = Number(like) || 0;

        const created = d && (d.created_at || d.createdAt || '');
        const metaEl = qs('articleMeta');
        if (!metaEl) return;

        const commentPart = currentCommentCount > 0
            ? '<span class="stat"><span class="stat-icon" aria-hidden="true">💬</span>' + currentCommentCount + '</span>'
            : '';

        metaEl.innerHTML = [
            '<span class="stat"><span class="stat-icon" aria-hidden="true">👁</span>' + currentViewCount + '</span>',
            '<span class="stat"><span class="stat-icon" aria-hidden="true">👍</span>' + currentLikeCount + '</span>',
            commentPart,
            created ? '<span class="stat">发布时间 ' + created + '</span>' : ''
        ].filter(Boolean).join(' · ');
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
            chip.className = 'tag-chip';
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

        // 安全：将内联 HTML 当作纯文本展示
        renderer.html = function (html) {
            if (html && typeof html === 'object') {
                return escapeHtml(html.text || '');
            }
            return escapeHtml(html);
        };

        // 兼容 marked 新旧版本的 code token
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
                // 保险：确保 renderer 生效
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
        // 允许中文、字母数字、下划线、短横线；其余去掉
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

        // 为标题补齐稳定 id（避免点击目录无法跳转）
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

            // 去重
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

    // 当前回复的父评论ID（"0" 表示直接评论文章）
    let currentParentId = '0';

    function setReplyTarget(parentId, userName) {
        currentParentId = String(parentId || '0');
        const msg = qs('commentMsg');
        if (!msg) return;
        if (currentParentId === '0') {
            msg.textContent = '';
        } else {
            msg.textContent = '回复 ' + (userName || '') + ' 中，提交后会作为该评论的回复。';
        }
        const form = qs('commentForm');
        if (form && form.content) {
            form.content.focus();
        }
    }

    function fmtTime(iso) {
        try {
            const d = new Date(iso);
            if (isNaN(d.getTime())) return '';
            return d.toLocaleString();
        } catch { return ''; }
    }

    function renderComments(list) {
        const root = qs('comments');
        root.innerHTML = '';

        const auth = getAuth();
        const currentUserId = auth && auth.userId ? String(auth.userId) : '';
        const hasLogin = auth && auth.token;

        if (!list || !list.length) {
            const empty = document.createElement('div');
            empty.className = 'muted';
            empty.textContent = '暂无评论';
            root.appendChild(empty);
            return;
        }

        list.forEach((c, index) => {
            const box = document.createElement('div');
            box.className = 'item';

            const header = document.createElement('div');
            header.className = 'row';
            header.style.justifyContent = 'space-between';
            const userName = (c.user && c.user.userName) ? c.user.userName : '匿名';
            const floorNo = index + 1;
            const createdRaw = c.createdAt || c.created_at || '';
            const createdText = createdRaw ? (fmtTime(createdRaw) || createdRaw) : '';

            const leftHead = document.createElement('div');
            leftHead.style.display = 'flex';
            leftHead.style.alignItems = 'center';
            leftHead.style.gap = '8px';
            const nameSpan = document.createElement('span');
            nameSpan.textContent = userName;
            leftHead.appendChild(nameSpan);

            const replyBtn = document.createElement('button');
            replyBtn.type = 'button';
            replyBtn.className = 'linklike';
            replyBtn.textContent = '↩';
            replyBtn.title = '回复';
            replyBtn.setAttribute('aria-label', '回复');
            replyBtn.onclick = function () {
                setReplyTarget(c.id, userName);
            };
            leftHead.appendChild(replyBtn);

            const commentOwnerId = c && c.user && c.user.userId ? String(c.user.userId) : '';
            if (hasLogin && currentUserId && commentOwnerId && currentUserId === commentOwnerId) {
                const delBtn = document.createElement('button');
                delBtn.type = 'button';
                delBtn.className = 'linklike';
                delBtn.setAttribute('aria-label', '删除评论');
                delBtn.textContent = '🗑';
                delBtn.onclick = async function () {
                    const ok = confirm('确定删除这条评论吗？');
                    if (!ok) return;
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
                leftHead.appendChild(delBtn);
            }

            const rightHead = document.createElement('div');
            rightHead.className = 'muted';
            rightHead.textContent = floorNo + '楼';

            header.appendChild(leftHead);
            header.appendChild(rightHead);

            const contentRow = document.createElement('div');
            contentRow.className = 'row';
            contentRow.style.justifyContent = 'space-between';
            contentRow.style.alignItems = 'flex-start';
            contentRow.style.gap = '12px';

            const content = document.createElement('div');
            content.style.flex = '1 1 auto';
            content.style.minWidth = '0';
            content.textContent = c.content || '';

            const time = document.createElement('div');
            time.className = 'muted';
            time.style.flex = '0 0 auto';
            time.style.whiteSpace = 'nowrap';
            time.textContent = createdText;

            contentRow.appendChild(content);
            if (createdText) contentRow.appendChild(time);

            box.appendChild(header);
            box.appendChild(contentRow);

            if (c.children && c.children.length) {
                const ul = document.createElement('ul');
                ul.className = 'list';
                c.children.forEach((child) => {
                    const li = document.createElement('li');
                    const u = (child.user && child.user.userName) ? child.user.userName : '匿名';
                    const ru = child.replyUser && child.replyUser.userName ? child.replyUser.userName : '';
                    const childCreatedRaw = child.createdAt || child.created_at || '';
                    const childCreatedText = childCreatedRaw ? (fmtTime(childCreatedRaw) || childCreatedRaw) : '';

                    const row1 = document.createElement('div');
                    row1.className = 'row';
                    row1.style.justifyContent = 'space-between';

                    const row1Left = document.createElement('div');
                    row1Left.style.display = 'flex';
                    row1Left.style.alignItems = 'center';
                    row1Left.style.gap = '8px';

                    const name = document.createElement('span');
                    name.textContent = ru ? (u + ' 回复 ' + ru) : u;
                    row1Left.appendChild(name);

                    const replyLink = document.createElement('button');
                    replyLink.type = 'button';
                    replyLink.className = 'linklike';
                    replyLink.textContent = '↩';
                    replyLink.title = '回复';
                    replyLink.setAttribute('aria-label', '回复');
                    replyLink.onclick = function () {
                        setReplyTarget(child.id, u);
                    };

                    row1Left.appendChild(replyLink);

                    const childOwnerId = child && child.user && child.user.userId ? String(child.user.userId) : '';
                    if (hasLogin && currentUserId && childOwnerId && currentUserId === childOwnerId) {
                        const delChildBtn = document.createElement('button');
                        delChildBtn.type = 'button';
                        delChildBtn.className = 'linklike';
                        delChildBtn.setAttribute('aria-label', '删除评论');
                        delChildBtn.textContent = '🗑';
                        delChildBtn.onclick = async function () {
                            const ok = confirm('确定删除这条评论吗？');
                            if (!ok) return;
                            const msg = qs('commentMsg');
                            if (msg) msg.textContent = '';
                            try {
                                await api('/api/v1/articles/comments/' + encodeURIComponent(String(child.id)), { method: 'DELETE' });
                                if (msg) msg.textContent = '已删除';
                                await loadComments();
                            } catch (e) {
                                if (msg) msg.textContent = '删除失败：' + e.message;
                            }
                        };
                        row1Left.appendChild(delChildBtn);
                    }

                    const row1Right = document.createElement('div');
                    row1Right.className = 'muted';
                    row1Right.textContent = '';

                    row1.appendChild(row1Left);
                    row1.appendChild(row1Right);

                    const row2 = document.createElement('div');
                    row2.className = 'row';
                    row2.style.justifyContent = 'space-between';
                    row2.style.alignItems = 'flex-start';
                    row2.style.gap = '12px';

                    const childContent = document.createElement('div');
                    childContent.style.flex = '1 1 auto';
                    childContent.style.minWidth = '0';
                    childContent.textContent = child.content || '';

                    const childTime = document.createElement('div');
                    childTime.className = 'muted';
                    childTime.style.flex = '0 0 auto';
                    childTime.style.whiteSpace = 'nowrap';
                    childTime.textContent = childCreatedText;

                    row2.appendChild(childContent);
                    if (childCreatedText) row2.appendChild(childTime);

                    li.appendChild(row1);
                    li.appendChild(row2);
                    ul.appendChild(li);
                });
                box.appendChild(ul);
            }

            root.appendChild(box);
        });
    }

    async function loadArticle() {
        const id = getArticleId();
        const resp = await api('/api/v1/articles/' + encodeURIComponent(id));
        const d = resp && resp.data;
        currentArticleData = d;

        setText('articleTitle', d.title || ('文章 ' + id));
        renderMeta(d, id);
        renderTags(d);
        const contentEl = qs('articleContent');
        if (contentEl) {
            // 与编辑器预览保持一致：优先用 marked 在前端渲染 Markdown
            const md = d.content || '';
            const html = renderMarkdown(md);
            if (html) {
                contentEl.innerHTML = html;
            } else if (d.html_content) {
                // 兜底：后端已渲染的 HTML
                contentEl.innerHTML = d.html_content;
            } else {
                contentEl.textContent = md;
            }

            // 生成目录（基于渲染后的 HTML 标题）
            buildTOCFromContent(contentEl);

            // 代码高亮
            applyHighlight(contentEl);
        }

        // 作者信息
        try {
            await loadAuthor(d.author_id);
        } catch (e) {
            setText('authorInfo', '作者信息加载失败：' + e.message);
        }

        // 点赞按钮初始化（需要登录）
        setupLikeButton(d);
    }

    function setupLikeButton(articleData) {
        const btn = qs('likeBtn');
        const msg = qs('likeMsg');
        if (!btn || !msg) return;

        const { token } = getAuth();
        msg.textContent = '';

        if (!token) {
            btn.disabled = true;
            btn.title = '点赞（需登录）';
            return;
        }

        btn.disabled = false;
        btn.title = '点赞';
        btn.onclick = async function () {
            const articleId = getArticleId();
            if (!articleId) return;

            btn.disabled = true;
            msg.textContent = '';

            // 前端先乐观 +1 展示
            const prevLike = currentLikeCount;
            currentLikeCount = prevLike + 1;
            renderMeta({ like_count: currentLikeCount, view_count: currentViewCount, author_id: articleData.author_id, created_at: articleData.created_at }, articleId);

            try {
                await api('/api/v1/articles/like', {
                    method: 'POST',
                    body: JSON.stringify({ article_id: String(articleId), is_cancel: false })
                });
                msg.textContent = '已点赞';
            } catch (e) {
                // 请求失败，回滚展示
                currentLikeCount = prevLike;
                renderMeta({ like_count: currentLikeCount, view_count: currentViewCount, author_id: articleData.author_id, created_at: articleData.created_at }, articleId);
                msg.textContent = '点赞失败：' + e.message;
            } finally {
                btn.disabled = false;
            }
        };
    }

    async function loadAuthor(authorId) {
        if (!authorId) {
            setText('authorInfo', '作者：-');
            setText('sideAuthorName', '-');
            // 清空右侧链接
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

        const authorEl = qs('authorInfo');
        if (authorEl) {
            const uid = String((u && (u.user_id || u.userId)) || authorId);
            authorEl.innerHTML = '作者：<a href="/u/' + encodeURIComponent(uid) + '">' + String(name) + '</a>';
        }

        // 右侧栏填充
        setText('sideAuthorName', name);
        const home = qs('sideHomeLink');
        const github = qs('sideGithubLink');
        const mail = qs('sideMailLink');
        const uid = String((u && (u.user_id || u.userId)) || authorId);
        if (home) home.href = '/u/' + encodeURIComponent(uid);
        bindGithubLink(github, u && (u.github || u.github_url || u.githubUrl));
        if (mail) {
            // 假设有私信页 /dm?to=uid
            mail.href = '/dm?to=' + encodeURIComponent(uid);
        }

        setupFollowButton(String((u && (u.user_id || u.userId)) || authorId));
    }

    function setupFollowButton(followingId) {
        const btn = qs('followBtn');
        const msg = qs('followMsg');
        msg.textContent = '';
        if (!btn) return;

        const { token, userId } = getAuth();
        // 未登录：隐藏按钮
        if (!token) {
            btn.style.display = 'none';
            return;
        }
        // 自己：不显示关注
        if (userId && String(userId) === String(followingId)) {
            btn.style.display = 'none';
            return;
        }

        btn.style.display = '';
        btn.disabled = false;
        btn.textContent = '关注';
        btn.onclick = async function () {
            msg.textContent = '';
            btn.disabled = true;
            try {
                await api('/api/v1/follow', {
                    method: 'POST',
                    body: JSON.stringify({ followingId: String(followingId) })
                });
                msg.textContent = '已关注';
                btn.textContent = '已关注';
            } catch (e) {
                msg.textContent = '关注失败：' + e.message;
                btn.disabled = false;
            }
        };
    }

    async function loadComments() {
        const id = getArticleId();
        const resp = await api('/api/v1/articles/comments?article_id=' + encodeURIComponent(id) + '&page=1&size=10&limit=2');
        const data = resp && resp.data;
        const list = data && (data.list || data) || [];
        renderComments(list);
        const total = data && (typeof data.total === 'number' ? data.total : list.length);
        currentCommentCount = total || 0;
        if (currentArticleData) {
            renderMeta(currentArticleData, id);
        }
    }

    async function submitComment(ev) {
        ev.preventDefault();

        const id = getArticleId();
        const { token, userId } = getAuth();
        const msg = qs('commentMsg');
        msg.textContent = '';

        if (!token) {
            msg.textContent = '请先登录再发表评论';
            return;
        }

        const content = String(ev.target.content.value || '').trim();
        if (!content) {
            msg.textContent = '评论不能为空';
            return;
        }

        try {
            await api('/api/v1/articles/comments', {
                method: 'POST',
                body: JSON.stringify({
                    articleId: String(id),
                    userId: String(userId || '0'),
                    parentId: String(currentParentId || '0'),
                    content
                })
            });

            ev.target.content.value = '';
            setReplyTarget('0', '');
            msg.textContent = '已发表';
            await loadComments();
        } catch (e) {
            msg.textContent = '发表失败：' + e.message;
        }
    }

    document.addEventListener('DOMContentLoaded', async function () {
        const form = qs('commentForm');
        form.addEventListener('submit', submitComment);

        try {
            await loadArticle();
        } catch (e) {
            setText('articleTitle', '加载失败');
            setText('articleContent', e.message);
        }

        try {
            await loadComments();
        } catch (e) {
            const root = qs('comments');
            root.innerHTML = '';
            const t = document.createElement('div');
            t.className = 'muted';
            t.textContent = '评论加载失败：' + e.message;
            root.appendChild(t);
        }
    });
})();
