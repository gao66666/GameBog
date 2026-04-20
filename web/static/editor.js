(function () {
    const { qs, api, getAuth } = window.GoBlog;

    function pageEl() { return qs('editorPage'); }

    function getArticleId() {
        const el = pageEl();
        return (el && el.getAttribute('data-article-id')) || '';
    }

    function setStatus(msg) {
        const el = qs('editorStatus');
        if (el) el.textContent = msg || '';
    }

    function setMsg(msg) {
        const el = qs('editorMsg');
        if (el) el.textContent = msg || '';
    }

    function syncPrimaryAction() {
        const btn = qs('saveArticleBtn');
        if (!btn) return;
        const isEdit = !!getArticleId();
        btn.textContent = isEdit ? '更新' : '发表';
        btn.title = isEdit ? '更新文章' : '发表文章';
    }

    function topicSelect() { return qs('editorTopicSelect'); }

    function setSelectedTopicId(topicId) {
        const sel = topicSelect();
        if (!sel) return;
        const want = String(topicId || '');
        if (!want) return;
        const opts = sel.options || [];
        for (let i = 0; i < opts.length; i++) {
            if (String(opts[i].value) === want) {
                sel.value = want;
                return;
            }
        }
    }

    function getSelectedTopicId() {
        const sel = topicSelect();
        if (!sel) return 0;
        const v = String(sel.value || '').trim();
        const n = Number(v);
        return Number.isFinite(n) && n > 0 ? n : 0;
    }

    function renderTopicOptions(topics) {
        const sel = topicSelect();
        if (!sel) return;

        sel.innerHTML = '';

        const list = Array.isArray(topics) ? topics : [];
        if (!list.length) {
            const opt = document.createElement('option');
            opt.value = '';
            opt.textContent = '暂无话题（请先到 /topics 创建）';
            sel.appendChild(opt);
            sel.disabled = true;
            return;
        }

        sel.disabled = false;
        list.forEach(t => {
            const id = t && (t.id ?? t.ID);
            const name = String((t && (t.name ?? t.Name)) || '').trim();
            if (!id || !name) return;
            const isTemp = !!(t && (t.isTemporary ?? t.is_temporary ?? t.IsTemporary));
            const opt = document.createElement('option');
            opt.value = String(id);
            opt.textContent = name + (isTemp ? '（临时）' : '');
            sel.appendChild(opt);
        });

        if (!sel.options.length) {
            const opt = document.createElement('option');
            opt.value = '';
            opt.textContent = '暂无话题（请先到 /topics 创建）';
            sel.appendChild(opt);
            sel.disabled = true;
        }
    }

    async function loadTopics(preferTopicId) {
        const sel = topicSelect();
        if (!sel) return;

        try {
            const resp = await api('/api/v1/topics');
            const topics = resp && resp.data && (resp.data.topics || resp.data.list || resp.data) || [];
            renderTopicOptions(topics);
            if (preferTopicId) {
                setSelectedTopicId(preferTopicId);
            }
        } catch (e) {
            renderTopicOptions([]);
            setMsg('话题加载失败：' + e.message);
        }
    }

    function normalizeTagNames(input) {
        const raw = String(input || '').trim();
        if (!raw) return [];
        const parts = raw.split(/\s+/g).map(s => String(s || '').trim()).filter(Boolean);
        const out = [];
        const seen = Object.create(null);
        for (let i = 0; i < parts.length; i++) {
            const t = parts[i];
            if (seen[t]) continue;
            seen[t] = true;
            out.push(t);
            if (out.length >= 5) break;
        }
        return out;
    }

    function dedupeTagNames(parts) {
        const out = [];
        const seen = Object.create(null);
        for (let i = 0; i < parts.length; i++) {
            const t = String(parts[i] || '').trim();
            if (!t || seen[t]) continue;
            seen[t] = true;
            out.push(t);
        }
        return out;
    }

    function readTagsFromUI() {
        const tagsInput = qs('editorTagsInput');
        const raw = tagsInput ? String(tagsInput.value || '') : '';
        const parts = String(raw || '').trim() ? String(raw || '').trim().split(/\s+/g).filter(Boolean) : [];
        const unique = dedupeTagNames(parts);
        if (unique.length > 5) throw new Error('标签最多 5 个');
        const tags = unique;

        // 回写为规范化格式（去重 + 单空格）
        if (tagsInput) tagsInput.value = tags.join(' ');
        return tags;
    }

    // 非依赖库的极简 Markdown 渲染（主要支持代码块 + 段落）
    function escapeHtml(str) {
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    }

    function hasMarked() {
        const m = window.marked;
        // marked 在不同版本/打包方式下可能是 function 或 object，但只要有 parse/setOptions 即可用
        return !!(m && typeof m.parse === 'function' && typeof m.setOptions === 'function');
    }

    function hasHLJS() {
        return window.hljs && typeof window.hljs.highlightElement === 'function';
    }

    function renderByMarked(text) {
        const m = window.marked;
        // 没有 Renderer 时，为了避免 inline HTML 带来的风险，直接走 fallback
        if (!m || typeof m.Renderer !== 'function') {
            return '';
        }

        const renderer = new m.Renderer();

        // 安全：将内联 HTML 当作纯文本展示，避免预览阶段执行脚本。
        renderer.html = function (html) {
            // marked 新版本可能传 token object：{ text, raw }
            if (html && typeof html === 'object') {
                return escapeHtml(html.text || '');
            }
            return escapeHtml(html);
        };

        // 规范化 fenced code 的 class：language-xxx
        renderer.code = function (code, infostring) {
            // marked 老版本：renderer.code(code: string, infostring: string)
            // marked 新版本：renderer.code(token: { text, lang, ... })
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
            // 避免反复覆盖全局配置
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
                // renderer 需要每次生效（否则 code/html 会用旧 renderer）
                m.setOptions({ renderer });
            }
            return m.parse(String(text || ''));
        } catch {
            return '';
        }
    }

    function simpleHighlight(preview) {
        const blocks = preview.querySelectorAll('pre code');
        blocks.forEach((codeEl) => {
            const cls = codeEl.className || '';
            if (cls.indexOf('language-python') === -1 && cls.indexOf('language-py') === -1) return;
            const src = codeEl.textContent;
            let html = src
                .replace(/(&)/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;');
            // strings
            html = html.replace(/("[^"]*"|'[^']*')/g, '<span class="code-str">$1</span>');
            // keywords (简易)
            const kw = '\\b(def|class|return|if|elif|else|for|while|import|from|as|pass|break|continue|in|is|not|and|or|None|True|False)\\b';
            html = html.replace(new RegExp(kw, 'g'), '<span class="code-kw">$1</span>');
            // numbers
            html = html.replace(/\b(\d+(?:\.\d+)?)\b/g, '<span class="code-num">$1</span>');
            codeEl.innerHTML = html;
        });
    }

    function simpleMarkdown(text) {
        const lines = String(text).replace(/\r\n?/g, '\n').split('\n');
        const out = [];
        let inCode = false;
        let codeLang = '';
        let codeBuf = [];

        function flushCode() {
            if (!codeBuf.length) return;
            const cls = codeLang ? 'language-' + codeLang : '';
            out.push('<pre><code class="' + cls + '">' + escapeHtml(codeBuf.join('\n')) + '</code></pre>');
            codeBuf = [];
        }

        function flushPara(buf) {
            if (!buf.length) return;
            const html = escapeHtml(buf.join(' '));
            out.push('<p>' + html + '</p>');
        }

        let paraBuf = [];

        for (let i = 0; i < lines.length; i++) {
            const line = lines[i];
            const fenceMatch = line.match(/^```\s*(\w+)?\s*$/);
            if (fenceMatch) {
                if (!inCode) {
                    // 开始代码块
                    flushPara(paraBuf);
                    paraBuf = [];
                    inCode = true;
                    codeLang = fenceMatch[1] || '';
                    continue;
                } else {
                    // 结束代码块
                    flushCode();
                    inCode = false;
                    codeLang = '';
                    continue;
                }
            }

            if (inCode) {
                codeBuf.push(line);
            } else {
                if (!line.trim()) {
                    flushPara(paraBuf);
                    paraBuf = [];
                } else {
                    paraBuf.push(line);
                }
            }
        }

        if (inCode) flushCode();
        if (paraBuf.length) flushPara(paraBuf);

        return out.join('\n');
    }

    function renderMarkdown(text) {
        if (hasMarked()) {
            const html = renderByMarked(text);
            if (html) return html;
        }
        return simpleMarkdown(text);
    }

    function applyHighlight(preview) {
        if (hasHLJS()) {
            try {
                if (!window.__goblog_hljs_inited) {
                    window.hljs.configure({ ignoreUnescapedHTML: true });
                    window.__goblog_hljs_inited = true;
                }
            } catch { /* ignore */ }

            const blocks = preview.querySelectorAll('pre code');
            blocks.forEach((codeEl) => {
                try { window.hljs.highlightElement(codeEl); } catch { /* ignore */ }
            });
            return;
        }

        // 兜底：只有 python 的极简高亮
        simpleHighlight(preview);
    }

    function renderPreview() {
        const input = qs('editorContentInput');
        const preview = qs('editorPreview');
        if (!input || !preview) return;
        const text = String(input.value || '');
        if (!text.trim()) {
            preview.innerHTML = '<div class="muted">在左侧输入内容，右侧将实时预览 Markdown 效果</div>';
            return;
        }
        const html = renderMarkdown(text);
        preview.innerHTML = html;
        applyHighlight(preview);
    }

    async function loadArticleForEdit(id) {
        try {
            setStatus('加载中...');
            const resp = await api('/api/v1/articles/' + encodeURIComponent(id));
            const d = resp && resp.data;
            if (!d) {
                setStatus('文章不存在');
                return;
            }
            const titleInput = qs('editorTitleInput');
            const summaryInput = qs('editorSummaryInput');
            const contentInput = qs('editorContentInput');
            const tagsInput = qs('editorTagsInput');
            if (titleInput) titleInput.value = d.title || '';
            if (summaryInput) summaryInput.value = d.summary || '';
            if (contentInput) contentInput.value = d.content || '';
            if (tagsInput) {
                const t = Array.isArray(d.tags) ? d.tags : [];
                const names = t.map(x => (x && x.name) ? String(x.name) : '').filter(Boolean);
                tagsInput.value = normalizeTagNames(names.join(' ')).join(' ');
            }

            const topicId = d.category_id || d.categoryId || d.categoryID || 0;
            if (topicId) {
                setSelectedTopicId(topicId);
            }
            renderPreview();
            setStatus('正在编辑文章 #' + id);
            const h1 = qs('editorTitle');
            if (h1) h1.textContent = '编辑文章';
            syncPrimaryAction();
        } catch (e) {
            setStatus('加载失败：' + e.message);
        }
    }

    async function saveArticle() {
        const { token, userId } = getAuth();
        if (!token || !userId) {
            setMsg('请先登录再编辑文章');
            return;
        }

        const titleInput = qs('editorTitleInput');
        const summaryInput = qs('editorSummaryInput');
        const contentInput = qs('editorContentInput');
        const title = String(titleInput.value || '').trim();
        const summary = String((summaryInput && summaryInput.value) || '').trim();
        const content = String(contentInput.value || '').trim();
        if (!title || !summary || !content) {
            setMsg('标题、摘要和内容不能为空');
            return;
        }

        let tags = [];
        try {
            tags = readTagsFromUI();
        } catch (e) {
            setMsg(e.message || '标签格式不正确');
            return;
        }

        const id = getArticleId();
        const isEdit = !!id;
        const url = isEdit
            ? '/api/v1/articles/' + encodeURIComponent(id)
            : '/api/v1/articles';
        const method = isEdit ? 'PUT' : 'POST';

        setMsg(isEdit ? '更新中...' : '发表中...');
        try {
            const topicId = getSelectedTopicId();
            if (!topicId) {
                setMsg('请选择话题');
                return;
            }

            const payload = { title, summary, content, tags, section_id: topicId };

            const resp = await api(url, {
                method,
                body: JSON.stringify(payload)
            });
            const data = resp && resp.data || {};
            const aid = data.article_id || data.id || id;
            if (!isEdit && aid) {
                const aidStr = String(aid);
                setStatus('已发表文章 #' + aidStr);
                setMsg('发表成功');

                // 取消“新建后跳转到文章页”；改为留在编辑页并切换为编辑态
                const page = pageEl();
                if (page) page.setAttribute('data-article-id', aidStr);
                const urlObj = new URL(window.location.href);
                urlObj.searchParams.set('id', aidStr);
                history.replaceState(null, '', urlObj.toString());
                syncPrimaryAction();
                return;
            }

            setStatus('更新成功');
            setMsg('更新成功');
        } catch (e) {
            setMsg((isEdit ? '更新失败：' : '发表失败：') + e.message);
        }
    }

    function bindImport() {
        const input = qs('editorImportFile');
        const contentInput = qs('editorContentInput');
        const titleInput = qs('editorTitleInput');
        if (!input || !contentInput) return;

        input.addEventListener('change', function (ev) {
            const file = ev.target.files && ev.target.files[0];
            if (!file) return;

            if (!/\.md$|\.txt$/i.test(file.name) && file.type !== 'text/plain') {
                setMsg('仅支持 .md 或 .txt 文件');
                ev.target.value = '';
                return;
            }

            const reader = new FileReader();
            reader.onload = function () {
                const text = String(reader.result || '');
                contentInput.value = text;
                if (titleInput && !titleInput.value) {
                    titleInput.value = file.name.replace(/\.(md|txt)$/i, '');
                }
                renderPreview();
                setMsg('已导入 ' + file.name);
            };
            reader.onerror = function () {
                setMsg('读取文件失败');
            };
            reader.readAsText(file, 'utf-8');
        });
    }

    function bindNew() {
        const btn = qs('newArticleBtn');
        if (!btn) return;
        btn.addEventListener('click', function () {
            const titleInput = qs('editorTitleInput');
            const summaryInput = qs('editorSummaryInput');
            const contentInput = qs('editorContentInput');
            if (titleInput) titleInput.value = '';
            if (summaryInput) summaryInput.value = '';
            if (contentInput) contentInput.value = '';

            const sel = topicSelect();
            if (sel && sel.options && sel.options.length) {
                sel.selectedIndex = 0;
            }

            const page = pageEl();
            if (page) page.setAttribute('data-article-id', '');
            const urlObj = new URL(window.location.href);
            urlObj.searchParams.delete('id');
            history.replaceState(null, '', urlObj.toString());
            renderPreview();
            setStatus('新建文章');
            setMsg('');
            syncPrimaryAction();
        });
    }

    document.addEventListener('DOMContentLoaded', async function () {
        const saveBtn = qs('saveArticleBtn');
        const contentInput = qs('editorContentInput');

        if (saveBtn) {
            saveBtn.addEventListener('click', function (ev) {
                ev.preventDefault();
                saveArticle();
            });
        }
        if (contentInput) {
            contentInput.addEventListener('input', renderPreview);
        }

        bindImport();
        bindNew();
        renderPreview();

        syncPrimaryAction();

        // 从查询参数里读取 topic_id / section_id：用于“从话题页新建文章”时预选当前话题
        const urlObj = new URL(window.location.href);
        const preferRaw = urlObj.searchParams.get('topic_id') || urlObj.searchParams.get('topicId') || urlObj.searchParams.get('section_id') || '';
        const preferTopicId = Number(preferRaw);

        // 先加载话题（编辑/新建都需要下拉）
        await loadTopics(Number.isFinite(preferTopicId) && preferTopicId > 0 ? preferTopicId : 0);

        // 从查询参数里读取 id 用于编辑
        const qid = urlObj.searchParams.get('id');
        const page = pageEl();
        if (qid && page) {
            page.setAttribute('data-article-id', qid);
        }
        const id = getArticleId();
        if (id) {
            await loadArticleForEdit(id);
        } else {
            setStatus('新建文章');
            syncPrimaryAction();
        }
    });
})();
