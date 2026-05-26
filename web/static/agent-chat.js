/**
 * GameBog Agent 聊天界面（侧栏历史会话 + SSE）
 */
(function () {
    'use strict';

    const qs = document.getElementById.bind(document);

    const chatMessages = qs('chatMessages');
    const chatMessagesInner = qs('chatMessagesInner');
    const chatThread = chatMessagesInner || chatMessages;
    const chatWelcome = qs('chatWelcome');
    const chatInput = qs('chatInput');
    const sendBtn = qs('sendBtn');
    const statusText = qs('statusText');
    const statusDot = qs('statusDot');
    const btnNewSession = qs('btnNewSession');
    const sessionListEl = qs('sessionList');

    let isSending = false;
    let abortController = null;
    let currentAssistantBubble = null;
    /** @type {string} */
    let currentSessionId = '';
    /** @type {Array<{session_id:string,title:string,updated_at:string}>} */
    let sessionsSnapshot = [];

    function getAuth() {
        return {
            token: localStorage.getItem('gb_token') || '',
            userId: localStorage.getItem('gb_user_id') || '',
            userName: localStorage.getItem('gb_user_name') || '',
        };
    }

    function formatSessionTime(iso) {
        if (!iso) return '';
        try {
            const d = new Date(iso);
            if (isNaN(d.getTime())) return '';
            const now = new Date();
            if (d.toDateString() === now.toDateString()) {
                return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
            }
            return d.getMonth() + 1 + '/' + d.getDate();
        } catch {
            return '';
        }
    }

    function renderSessionRows() {
        if (!sessionListEl) return;
        sessionListEl.innerHTML = '';
        const auth = getAuth();
        if (!auth.token) {
            sessionListEl.innerHTML = '<div class="agent-session-empty">登录后查看历史会话</div>';
            return;
        }
        if (!sessionsSnapshot.length) {
            sessionListEl.innerHTML = '<div class="agent-session-empty">暂无会话，点「新对话」开始</div>';
            return;
        }
        sessionsSnapshot.forEach(function (s) {
            const sid = s.session_id || '';
            const row = document.createElement('div');
            row.className = 'agent-session-row' + (sid === currentSessionId ? ' active' : '');
            row.dataset.sessionId = sid;

            const main = document.createElement('div');
            main.className = 'agent-session-row-main';
            const titleEl = document.createElement('div');
            titleEl.className = 'agent-session-row-title';
            titleEl.textContent = s.title || '新对话';
            const timeEl = document.createElement('div');
            timeEl.className = 'agent-session-row-time';
            timeEl.textContent = formatSessionTime(s.updated_at);
            main.appendChild(titleEl);
            main.appendChild(timeEl);

            const delBtn = document.createElement('button');
            delBtn.type = 'button';
            delBtn.className = 'agent-session-row-del';
            delBtn.title = '删除会话';
            delBtn.setAttribute('aria-label', '删除');
            delBtn.textContent = '×';

            row.appendChild(main);
            row.appendChild(delBtn);

            row.addEventListener('click', function () {
                if (sid && sid !== currentSessionId) {
                    selectSession(sid);
                }
            });

            delBtn.addEventListener('click', function (ev) {
                ev.stopPropagation();
                deleteSessionById(sid);
            });

            sessionListEl.appendChild(row);
        });
    }

    async function refreshSessionList() {
        const auth = getAuth();
        if (!auth.token) {
            sessionsSnapshot = [];
            currentSessionId = '';
            renderSessionRows();
            return;
        }
        try {
            const resp = await fetch('/api/v1/agent/sessions', {
                headers: { Authorization: 'Bearer ' + auth.token },
                cache: 'no-store',
            });
            if (!resp.ok) return;
            const body = await resp.json();
            if (body && body.code !== 0 && body.code !== undefined) return;
            const data = body.data || {};
            sessionsSnapshot = data.sessions || [];
            if (data.current_session_id) {
                currentSessionId = data.current_session_id;
            }
            renderSessionRows();
        } catch {
            /* ignore */
        }
    }

    async function selectSession(sid) {
        if (!sid || sid === currentSessionId) return;
        currentSessionId = sid;
        renderSessionRows();
        resetChatView();
        await loadHistory();
    }

    async function deleteSessionById(sid) {
        if (!sid) return;
        if (!confirm('确定删除该会话？')) return;
        const auth = getAuth();
        if (!auth.token) return;
        try {
            const resp = await fetch('/api/v1/agent/sessions/' + encodeURIComponent(sid), {
                method: 'DELETE',
                headers: { Authorization: 'Bearer ' + auth.token },
            });
            const body = await resp.json().catch(function () {
                return {};
            });
            if (!resp.ok || (body.code !== 0 && body.code !== undefined)) {
                throw new Error(body.message || body.msg || '删除失败');
            }
            const data = body.data || {};
            if (data.current_session_id !== undefined) {
                currentSessionId = data.current_session_id || '';
            }
            await refreshSessionList();
            resetChatView();
            await loadHistory();
        } catch (e) {
            alert('删除失败：' + (e.message || e));
        }
    }

    function autoResize(el) {
        el.style.height = 'auto';
        el.style.height = Math.min(el.scrollHeight, 160) + 'px';
    }

    function addMessage(role, content) {
        chatWelcome.style.display = 'none';
        chatMessages.style.display = 'block';

        const div = document.createElement('div');
        div.className = 'chat-msg ' + role;

        const avatar = document.createElement('div');
        avatar.className = 'avatar';
        avatar.textContent = role === 'assistant' ? '博' : getAuth().userName?.charAt(0) || '我';
        div.appendChild(avatar);

        const bubble = document.createElement('div');
        bubble.className = 'bubble';
        bubble.textContent = content;
        div.appendChild(bubble);

        chatThread.appendChild(div);
        scrollToBottom();
        return bubble;
    }

    function appendToAssistant(text) {
        chatWelcome.style.display = 'none';
        chatMessages.style.display = 'block';

        if (!currentAssistantBubble) {
            const div = document.createElement('div');
            div.className = 'chat-msg assistant';

            const avatar = document.createElement('div');
            avatar.className = 'avatar';
            avatar.textContent = '博';
            div.appendChild(avatar);

            const bubble = document.createElement('div');
            bubble.className = 'bubble';
            bubble.textContent = '';
            div.appendChild(bubble);

            chatThread.appendChild(div);
            currentAssistantBubble = bubble;
        }

        currentAssistantBubble.textContent += text;
        scrollToBottom();
    }

    function showThinking() {
        hideThinking();
        const div = document.createElement('div');
        div.className = 'chat-msg assistant';
        div.id = 'thinkingIndicator';
        const avatar = document.createElement('div');
        avatar.className = 'avatar';
        avatar.textContent = '博';
        div.appendChild(avatar);
        const bubble = document.createElement('div');
        bubble.className = 'chat-thinking';
        bubble.innerHTML = '<span>思考中</span><div class="dots"><span></span><span></span><span></span></div>';
        div.appendChild(bubble);
        chatThread.appendChild(div);
        scrollToBottom();
    }

    function hideThinking() {
        const el = qs('thinkingIndicator');
        if (el) el.remove();
    }

    function showToolCall(toolName) {
        const div = document.createElement('div');
        div.className = 'chat-tool-call';
        div.innerHTML = '<span class="tool-icon">🔧</span> 正在调用: ' + toolName + '…';
        chatThread.appendChild(div);
        scrollToBottom();
    }

    function scrollToBottom() {
        requestAnimationFrame(function () {
            const parent = chatMessages.style.display !== 'none' ? chatMessages : chatWelcome;
            parent.scrollTop = parent.scrollHeight;
        });
    }

    function setSending(sending) {
        isSending = sending;
        sendBtn.disabled = sending || !chatInput.value.trim();
        sendBtn.innerHTML = sending
            ? '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="2" x2="12" y2="6"/><line x1="12" y1="18" x2="12" y2="22"/><line x1="4.93" y1="4.93" x2="7.76" y2="7.76"/><line x1="16.24" y1="16.24" x2="19.07" y2="19.07"/><line x1="2" y1="12" x2="6" y2="12"/><line x1="18" y1="12" x2="22" y2="12"/><line x1="4.93" y1="19.07" x2="7.76" y2="16.24"/><line x1="16.24" y1="7.76" x2="19.07" y2="4.93"/></svg>'
            : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>';
        chatInput.disabled = sending;
        if (!sending) chatInput.focus();
    }

    function updateStatus(online) {
        statusDot.className = 'dot' + (online ? '' : ' offline');
        statusText.textContent = online ? '已连接' : '离线模式（消息将发送）';
    }

    async function sendMessage(text) {
        if (isSending || !text.trim()) return;

        const auth = getAuth();
        const msg = text.trim();

        if (!auth.token) {
            addMessage('assistant', '请先登录后再使用 AI 助手（需要携带登录令牌）。');
            setSending(false);
            hideThinking();
            return;
        }

        chatInput.value = '';
        autoResize(chatInput);
        sendBtn.disabled = true;

        addMessage('user', msg);

        setSending(true);
        showThinking();

        abortController = new AbortController();

        try {
            const payload = {
                message: msg,
                token: auth.token || '',
            };
            if (currentSessionId) {
                payload.chat_session_id = currentSessionId;
            }

            const resp = await fetch('/api/v1/agent/chat', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    Authorization: 'Bearer ' + auth.token,
                },
                body: JSON.stringify(payload),
                signal: abortController.signal,
            });

            if (!resp.ok) {
                const errText = await resp.text().catch(function () {
                    return '';
                });
                throw new Error(errText || 'HTTP ' + resp.status);
            }

            const reader = resp.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';

                for (let i = 0; i < lines.length; i++) {
                    const trimmed = lines[i].trim();
                    if (!trimmed.startsWith('data: ')) continue;

                    const jsonStr = trimmed.slice(6);
                    try {
                        const event = JSON.parse(jsonStr);
                        handleSSEEvent(event);
                    } catch {
                        /* ignore */
                    }
                }
            }

            if (buffer.trim()) {
                const trimmed = buffer.trim();
                if (trimmed.startsWith('data: ')) {
                    try {
                        const event = JSON.parse(trimmed.slice(6));
                        handleSSEEvent(event);
                    } catch {
                        /* ignore */
                    }
                }
            }
        } catch (err) {
            if (err.name === 'AbortError') return;
            hideThinking();
            appendToAssistant('\n\n[错误: ' + err.message + ']');
        } finally {
            hideThinking();
            currentAssistantBubble = null;
            setSending(false);
            abortController = null;
            updateSendBtn();
            refreshSessionList();
        }
    }

    function handleSSEEvent(event) {
        switch (event.type) {
            case 'session':
            case 'rewrite':
            case 'route':
                break;
            case 'thinking':
                if (event.content) statusText.textContent = event.content;
                showThinking();
                break;
            case 'phase':
                if (event.content) statusText.textContent = event.content;
                showThinking();
                break;
            case 'text_chunk':
            case 'token':
                hideThinking();
                appendToAssistant(event.content || '');
                break;
            case 'tool_start':
                hideThinking();
                showToolCall(event.content || event.tool || 'unknown');
                break;
            case 'tool_end': {
                const toolCalls = chatThread.querySelectorAll('.chat-tool-call');
                if (toolCalls.length > 0 && event.content) {
                    toolCalls[toolCalls.length - 1].textContent = event.content;
                }
                break;
            }
            case 'done':
                hideThinking();
                break;
            case 'error':
                hideThinking();
                appendToAssistant('\n\n[错误: ' + (event.content || '未知错误') + ']');
                break;
        }
    }

    function updateSendBtn() {
        sendBtn.disabled = isSending || !chatInput.value.trim();
    }

    function resetChatView() {
        if (abortController) {
            abortController.abort();
            abortController = null;
        }
        chatThread.innerHTML = '';
        chatMessages.style.display = 'none';
        chatWelcome.style.display = 'flex';
        currentAssistantBubble = null;
        hideThinking();
        isSending = false;
        setSending(false);
        updateSendBtn();
    }

    chatInput.addEventListener('input', function () {
        autoResize(this);
        updateSendBtn();
    });

    chatInput.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            if (!isSending && this.value.trim()) {
                sendMessage(this.value);
            }
        }
    });

    sendBtn.addEventListener('click', function () {
        if (!isSending && chatInput.value.trim()) {
            sendMessage(chatInput.value);
        }
    });

    if (btnNewSession) {
        btnNewSession.addEventListener('click', async function () {
            if (isSending) return;
            const auth = getAuth();
            if (!auth.token) {
                alert('请先登录后再新建会话。');
                return;
            }
            try {
                const resp = await fetch('/api/v1/agent/sessions', {
                    method: 'POST',
                    headers: { Authorization: 'Bearer ' + auth.token },
                });
                const body = await resp.json();
                if (!resp.ok || (body.code !== 0 && body.code !== undefined)) {
                    throw new Error(body.message || body.msg || '创建失败');
                }
                const sid = (body.data && body.data.session_id) || '';
                if (sid) {
                    currentSessionId = sid;
                }
                resetChatView();
                await refreshSessionList();
            } catch (e) {
                alert('新建会话失败：' + (e.message || e));
            }
        });
    }

    document.querySelectorAll('.hint-chip').forEach(function (chip) {
        chip.addEventListener('click', function () {
            const hint = this.dataset.hint;
            if (hint && !isSending) {
                sendMessage(hint);
            }
        });
    });

    async function loadHistory() {
        const auth = getAuth();
        if (!auth.token) return;
        try {
            let url = '/api/v1/agent/history?limit=48';
            if (currentSessionId) {
                url += '&session_id=' + encodeURIComponent(currentSessionId);
            }
            const resp = await fetch(url, {
                method: 'GET',
                headers: { Authorization: 'Bearer ' + auth.token },
                cache: 'no-store',
            });
            if (!resp.ok) return;
            const body = await resp.json();
            if (body && body.code !== 0 && body.code !== undefined) return;
            const data = body.data || {};
            if (data.session_id) {
                currentSessionId = data.session_id;
                renderSessionRows();
            }
            const messages = data.messages || [];

            chatThread.innerHTML = '';
            if (!messages.length) {
                chatWelcome.style.display = 'flex';
                chatMessages.style.display = 'none';
                return;
            }

            chatWelcome.style.display = 'none';
            chatMessages.style.display = 'block';

            messages.forEach(function (m) {
                const role = m.role === 'assistant' ? 'assistant' : 'user';
                const content = String(m.content || '').trim();
                if (!content) return;
                addMessage(role, content);
            });
            scrollToBottom();
        } catch {
            /* ignore */
        }
    }

    async function boot() {
        updateStatus(true);
        await refreshSessionList();
        await loadHistory();
        chatInput.focus();
        updateSendBtn();

        window.addEventListener('storage', function (e) {
            if (e.key === 'gb_token' || e.key === 'gb_user_id') {
                if (e.key === 'gb_token') {
                    refreshSessionList().then(function () {
                        return loadHistory();
                    });
                }
            }
        });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', boot);
    } else {
        boot();
    }
})();
