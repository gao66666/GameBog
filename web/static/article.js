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

    function setText(id, text) {
        const el = qs(id);
        if (el) el.textContent = text;
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
            const userName = (c.user && c.user.userName) ? c.user.userName : '匿名';
            const floorNo = index + 1;
            const createdRaw = c.createdAt || c.created_at || '';
            const createdText = createdRaw ? (fmtTime(createdRaw) || createdRaw) : '';

            const metaSpan = document.createElement('span');
            // 顺序：#楼层、时间、评论人姓名
            metaSpan.textContent = '#' + floorNo + (createdText ? (' · ' + createdText) : '') + ' · ' + userName;

            header.appendChild(metaSpan);

            const replyBtn = document.createElement('button');
            replyBtn.type = 'button';
            replyBtn.className = 'linklike';
            replyBtn.style.marginLeft = '8px';
            replyBtn.textContent = '回复';
            replyBtn.onclick = function () {
                setReplyTarget(c.id, userName);
            };
            header.appendChild(replyBtn);

            const commentOwnerId = c && c.user && c.user.userId ? String(c.user.userId) : '';
            if (hasLogin && currentUserId && commentOwnerId && currentUserId === commentOwnerId) {
                const delBtn = document.createElement('button');
                delBtn.type = 'button';
                delBtn.className = 'linklike';
                delBtn.style.marginLeft = '8px';
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
                header.appendChild(delBtn);
            }

            const content = document.createElement('div');
            content.textContent = c.content || '';

            box.appendChild(header);
            box.appendChild(content);

            if (c.children && c.children.length) {
                const ul = document.createElement('ul');
                ul.className = 'list';
                c.children.forEach((child) => {
                    const li = document.createElement('li');
                    const u = (child.user && child.user.userName) ? child.user.userName : '匿名';
                    const ru = child.replyUser && child.replyUser.userName ? child.replyUser.userName : '';
                    const text = document.createElement('span');
                    text.textContent = (ru ? (u + ' 回复 ' + ru + '：') : (u + '：')) + (child.content || '');

                    const replyLink = document.createElement('button');
                    replyLink.type = 'button';
                    replyLink.className = 'linklike';
                    replyLink.style.marginLeft = '8px';
                    replyLink.textContent = '回复';
                    replyLink.onclick = function () {
                        setReplyTarget(child.id, u);
                    };

                    li.appendChild(text);

                    const childOwnerId = child && child.user && child.user.userId ? String(child.user.userId) : '';
                    if (hasLogin && currentUserId && childOwnerId && currentUserId === childOwnerId) {
                        const delChildBtn = document.createElement('button');
                        delChildBtn.type = 'button';
                        delChildBtn.className = 'linklike';
                        delChildBtn.style.marginLeft = '8px';
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
                        li.appendChild(delChildBtn);
                    }
                    li.appendChild(replyLink);
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
        const contentEl = qs('articleContent');
        if (contentEl) {
            if (d.html_content) {
                contentEl.innerHTML = d.html_content;
            } else {
                contentEl.textContent = d.content || '';
            }
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
