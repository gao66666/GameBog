/**
 * GameBog AI 助手浮动侧边栏
 * 全局可用，从右侧滑出，SSE 流式对话
 */
(function () {
    'use strict';

    // 只在非 agent 页面加载
    if (location.pathname === '/agent') return;

    var SIDEBAR_HTML = [
        '<div id="agentSidebarOverlay" class="as-overlay"></div>',
        '<button id="agentSidebarBtn" class="as-float-btn" title="AI 助手">',
        '<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">',
        '<path d="M12 2a4 4 0 0 1 4 4v2a4 4 0 0 1-8 0V6a4 4 0 0 1 4-4z"/>',
        '<path d="M16 14H8a4 4 0 0 0-4 4v2h16v-2a4 4 0 0 0-4-4z"/>',
        '</svg>',
        '</button>',
        '<div id="agentSidebar" class="as-sidebar">',
        '<div class="as-header">',
        '<span class="as-title">🤖 小博</span>',
        '<button id="asClose" class="as-close" title="关闭">✕</button>',
        '</div>',
        '<div class="as-body">',
        '<div class="as-welcome" id="asWelcome">',
        '<div class="as-welcome-icon">🤖</div>',
        '<div class="as-welcome-title">小博</div>',
        '<div class="as-welcome-desc">GameBog 智能助手，帮你搜索文章、浏览话题、查找游戏。</div>',
        '<div class="as-hints">',
        '<div class="as-hint" data-hint="最近有什么热门文章？">🔥 热门文章</div>',
        '<div class="as-hint" data-hint="有哪些话题可以看？">📂 浏览话题</div>',
        '<div class="as-hint" data-hint="有什么好玩的游戏？">🎮 游戏推荐</div>',
        '<div class="as-hint" data-hint="帮我搜一下关于Go的文章">🔍 搜索文章</div>',
        '</div>',
        '</div>',
        '<div class="as-msgs" id="asMsgs"></div>',
        '<div class="as-thinking" id="asThinking" style="display:none">',
        '<span class="as-thinking-dots"><span></span><span></span><span></span></span>',
        '</div>',
        '</div>',
        '<div class="as-footer">',
        '<div class="as-input-wrap">',
        '<textarea id="asInput" rows="1" placeholder="输入消息…" class="as-input"></textarea>',
        '<button id="asSendBtn" class="as-send-btn" disabled>',
        '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>',
        '</button>',
        '</div>',
        '</div>',
        '</div>',
    ].join('');

    var STYLE = [
        '#agentSidebarOverlay {',
        'position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.15);z-index:9998;',
        'opacity:0;visibility:hidden;transition:opacity .25s,visibility .25s;',
        '}',
        '#agentSidebarOverlay.open { opacity:1;visibility:visible; }',
        '.as-float-btn {',
        'position:fixed;bottom:24px;right:24px;z-index:9997;',
        'width:50px;height:50px;border-radius:50%;border:none;',
        'background:linear-gradient(135deg,#667eea,#764ba2);color:#fff;',
        'cursor:pointer;box-shadow:0 4px 16px rgba(102,126,234,0.35);',
        'display:flex;align-items:center;justify-content:center;',
        'transition:transform .2s,box-shadow .2s;',
        '}',
        '.as-float-btn:hover { transform:scale(1.08);box-shadow:0 6px 24px rgba(102,126,234,0.45); }',
        '.as-float-btn:active { transform:scale(0.95); }',
        '.as-sidebar {',
        'position:fixed;top:0;right:0;z-index:9999;',
        'width:400px;height:100%;max-width:100vw;',
        'background:#fff;box-shadow:-4px 0 30px rgba(0,0,0,0.1);',
        'display:flex;flex-direction:column;',
        'transform:translateX(100%);transition:transform .3s cubic-bezier(0.4,0,0.2,1);',
        '}',
        '.as-sidebar.open { transform:translateX(0); }',
        '.as-header {',
        'display:flex;align-items:center;justify-content:space-between;',
        'padding:16px 20px;border-bottom:1px solid #e8e8ed;flex-shrink:0;',
        '}',
        '.as-title { font-size:16px;font-weight:700; }',
        '.as-close {',
        'width:32px;height:32px;border-radius:50%;border:none;',
        'background:transparent;color:#86868b;font-size:18px;',
        'cursor:pointer;display:flex;align-items:center;justify-content:center;',
        'transition:background .2s;',
        '}',
        '.as-close:hover { background:#f5f5f7;color:#1d1d1f; }',
        '.as-body { flex:1;overflow-y:auto;padding:16px;min-height:0; }',
        '.as-body::-webkit-scrollbar { width:5px; }',
        '.as-body::-webkit-scrollbar-thumb { background:#e8e8ed;border-radius:3px; }',
        '.as-welcome { text-align:center;padding:40px 16px; }',
        '.as-welcome-icon { font-size:40px;margin-bottom:10px; }',
        '.as-welcome-title { font-size:20px;font-weight:700;margin-bottom:6px;background:linear-gradient(135deg,#667eea,#764ba2);-webkit-background-clip:text;-webkit-text-fill-color:transparent;background-clip:text; }',
        '.as-welcome-desc { font-size:13px;color:#86868b;line-height:1.5;margin-bottom:18px; }',
        '.as-hints { display:flex;flex-wrap:wrap;gap:6px;justify-content:center; }',
        '.as-hint { padding:6px 12px;border-radius:999px;border:1px solid #e8e8ed;font-size:12px;color:#86868b;cursor:pointer;transition:all .2s;background:#fff; }',
        '.as-hint:hover { border-color:#667eea;color:#667eea; }',
        '.as-msgs { display:flex;flex-direction:column;gap:14px; }',
        '.as-msg { display:flex;gap:10px;animation:asIn .25s ease; }',
        '@keyframes asIn { from{opacity:0;transform:translateY(6px)} to{opacity:1;transform:translateY(0)} }',
        '.as-msg-user { flex-direction:row-reverse; }',
        '.as-msg-avatar { width:28px;height:28px;border-radius:50%;flex-shrink:0;display:flex;align-items:center;justify-content:center;font-size:12px;font-weight:600;margin-top:2px; }',
        '.as-msg-bot .as-msg-avatar { background:linear-gradient(135deg,#667eea,#764ba2);color:#fff; }',
        '.as-msg-user .as-msg-avatar { background:#e8e8ed;color:#555; }',
        '.as-msg-bubble { max-width:85%;padding:8px 14px;border-radius:14px;font-size:14px;line-height:1.55;white-space:pre-wrap;word-break:break-word; }',
        '.as-msg-bot .as-msg-bubble { background:#f5f5f7;color:#1d1d1f;border-top-left-radius:4px; }',
        '.as-msg-user .as-msg-bubble { background:#0071e3;color:#fff;border-top-right-radius:4px; }',
        '.as-thinking { display:flex;justify-content:flex-start;padding:8px 38px; }',
        '.as-thinking-dots span { display:inline-block;width:6px;height:6px;border-radius:50%;background:#c7c7cc;margin:0 2px;animation:asDot 1.4s infinite; }',
        '.as-thinking-dots span:nth-child(2) { animation-delay:.2s; }',
        '.as-thinking-dots span:nth-child(3) { animation-delay:.4s; }',
        '@keyframes asDot { 0%,80%,100%{transform:scale(0)} 40%{transform:scale(1)} }',
        '.as-footer { flex-shrink:0;border-top:1px solid #e8e8ed;padding:12px 16px; }',
        '.as-input-wrap { display:flex;align-items:flex-end;gap:8px;background:#f5f5f7;border-radius:12px;padding:8px 10px; }',
        '.as-input { flex:1;border:none;outline:none;resize:none;font-size:14px;font-family:inherit;line-height:1.4;padding:2px 0;min-height:20px;max-height:80px;background:transparent; }',
        '.as-send-btn { width:30px;height:30px;border-radius:50%;border:none;background:#0071e3;color:#fff;cursor:pointer;display:flex;align-items:center;justify-content:center;flex-shrink:0;transition:background .2s,transform .1s; }',
        '.as-send-btn:hover { background:#0058b0; }',
        '.as-send-btn:active { transform:scale(.9); }',
        '.as-send-btn:disabled { background:#d1d1d6;cursor:default; }',
        '@media (max-width:480px) { .as-sidebar { width:100vw; } }',
    ].join('\n');

    // ============================================================
    // 状态
    // ============================================================

    var isOpen = false;
    var isSending = false;
    var abortCtrl = null;
    var currentBubble = null;

    // ============================================================
    // 注入 DOM
    // ============================================================

    function inject() {
        if (document.getElementById('agentSidebar')) return;

        var style = document.createElement('style');
        style.textContent = STYLE;
        document.head.appendChild(style);

        var div = document.createElement('div');
        div.innerHTML = SIDEBAR_HTML;
        while (div.firstChild) document.body.appendChild(div.firstChild);
    }

    // ============================================================
    // 帮助函数
    // ============================================================

    function el(id) { return document.getElementById(id); }
    function auth() {
        return {
            token: localStorage.getItem('gb_token') || '',
            userId: localStorage.getItem('gb_user_id') || '',
            userName: localStorage.getItem('gb_user_name') || '',
        };
    }

    function scrollBottom() {
        var body = el('agentSidebar').querySelector('.as-body');
        requestAnimationFrame(function () { body.scrollTop = body.scrollHeight; });
    }

    function addMsg(role, content) {
        el('asWelcome').style.display = 'none';
        var container = el('asMsgs');

        var div = document.createElement('div');
        div.className = 'as-msg as-msg-' + role;

        var avatar = document.createElement('div');
        avatar.className = 'as-msg-avatar';
        avatar.textContent = role === 'bot' ? '博' : (auth().userName ? auth().userName.charAt(0) : '我');
        div.appendChild(avatar);

        var bubble = document.createElement('div');
        bubble.className = 'as-msg-bubble';
        bubble.textContent = content;
        div.appendChild(bubble);

        container.appendChild(div);
        scrollBottom();
    }

    function streamText(text) {
        el('asWelcome').style.display = 'none';
        var container = el('asMsgs');

        if (!currentBubble) {
            var div = document.createElement('div');
            div.className = 'as-msg as-msg-bot';

            var avatar = document.createElement('div');
            avatar.className = 'as-msg-avatar';
            avatar.textContent = '博';
            div.appendChild(avatar);

            var bubble = document.createElement('div');
            bubble.className = 'as-msg-bubble';
            bubble.textContent = '';
            div.appendChild(bubble);

            container.appendChild(div);
            currentBubble = bubble;
        }

        currentBubble.textContent += text;
        scrollBottom();
    }

    function showThinking(on) {
        var t = el('asThinking');
        if (t) t.style.display = on ? '' : 'none';
        if (on) scrollBottom();
    }

    function setSending(v) {
        isSending = v;
        var btn = el('asSendBtn');
        if (btn) btn.disabled = v || !(el('asInput') && el('asInput').value.trim());
        if (el('asInput')) el('asInput').disabled = v;
    }

    /** 打开侧栏时从服务端拉 Redis 中的短期对话（需登录） */
    function loadSidebarHistory() {
        var a = auth();
        var welcome = el('asWelcome');
        var msgs = el('asMsgs');
        if (!msgs) return;
        if (!a.token) {
            if (welcome) welcome.style.display = '';
            return;
        }
        fetch('/api/v1/agent/history?limit=48', {
            headers: { Authorization: 'Bearer ' + a.token },
            cache: 'no-store',
        }).then(function (resp) {
            if (!resp.ok) return Promise.reject();
            return resp.json();
        }).then(function (body) {
            if (body && body.code !== 0 && body.code !== undefined) return;
            var data = body.data || {};
            var list = data.messages || [];
            msgs.innerHTML = '';
            if (!list.length) {
                if (welcome) welcome.style.display = '';
                return;
            }
            if (welcome) welcome.style.display = 'none';
            list.forEach(function (m) {
                var content = String((m && m.content) || '').trim();
                if (!content) return;
                if (m.role === 'assistant') addMsg('bot', content);
                else if (m.role === 'user') addMsg('user', content);
            });
            scrollBottom();
        }).catch(function () { /* 静默 */ });
    }

    // ============================================================
    // SSE 请求
    // ============================================================

    async function send(text) {
        if (isSending || !text.trim()) return;
        var a = auth();
        if (!a.token) {
            addMsg('bot', '请先登录后再使用 AI 助手。');
            return;
        }
        var msg = text.trim();
        el('asInput').value = '';
        el('asInput').style.height = 'auto';
        setSending(true);

        addMsg('user', msg);
        showThinking(true);

        abortCtrl = new AbortController();

        try {
            var resp = await fetch('/api/v1/agent/chat', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    Authorization: 'Bearer ' + a.token,
                },
                body: JSON.stringify({ message: msg, token: a.token || '' }),
                signal: abortCtrl.signal,
            });

            if (!resp.ok) throw new Error('HTTP ' + resp.status);

            var reader = resp.body.getReader();
            var decoder = new TextDecoder();
            var buf = '';

            while (true) {
                var r = await reader.read();
                if (r.done) break;
                buf += decoder.decode(r.value, { stream: true });
                var lines = buf.split('\n');
                buf = lines.pop() || '';

                for (var i = 0; i < lines.length; i++) {
                    var ln = lines[i].trim();
                    if (!ln.startsWith('data: ')) continue;
                    try {
                        var ev = JSON.parse(ln.slice(6));
                        if (ev.type === 'token') {
                            showThinking(false);
                            streamText(ev.content || '');
                        }
                    } catch (e) { /* skip */ }
                }
            }
        } catch (e) {
            if (e.name === 'AbortError') return;
            showThinking(false);
            streamText('\n[错误: ' + e.message + ']');
        } finally {
            showThinking(false);
            currentBubble = null;
            setSending(false);
        }
    }

    // ============================================================
    // 绑定事件
    // ============================================================

    function bind() {
        var overlay = el('agentSidebarOverlay');
        var sidebar = el('agentSidebar');
        var btn = el('agentSidebarBtn');
        var close = el('asClose');
        var input = el('asInput');
        var sendBtn = el('asSendBtn');

        function open() {
            isOpen = true;
            sidebar.classList.add('open');
            overlay.classList.add('open');
            document.body.style.overflow = 'hidden';
            loadSidebarHistory();
            if (input) { input.focus(); }
        }

        function closeFn() {
            isOpen = false;
            sidebar.classList.remove('open');
            overlay.classList.remove('open');
            document.body.style.overflow = '';
            if (abortCtrl) { abortCtrl.abort(); abortCtrl = null; }
        }

        if (btn) btn.addEventListener('click', open);
        if (close) close.addEventListener('click', closeFn);
        if (overlay) overlay.addEventListener('click', closeFn);

        if (input) {
            input.addEventListener('input', function () {
                this.style.height = 'auto';
                this.style.height = Math.min(this.scrollHeight, 80) + 'px';
                if (sendBtn) sendBtn.disabled = isSending || !this.value.trim();
            });
            input.addEventListener('keydown', function (e) {
                if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault();
                    if (!isSending && this.value.trim()) send(this.value);
                }
            });
        }

        if (sendBtn) {
            sendBtn.addEventListener('click', function () {
                if (!isSending && input && input.value.trim()) send(input.value);
            });
        }

        document.querySelectorAll('.as-hint').forEach(function (chip) {
            chip.addEventListener('click', function () {
                var hint = this.getAttribute('data-hint');
                if (hint && !isSending) send(hint);
            });
        });
    }

    // ============================================================
    // 启动
    // ============================================================

    function init() {
        inject();
        bind();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
