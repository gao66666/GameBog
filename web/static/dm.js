(function () {
    const { qs, api, getAuth, ensureWSConnected } = window.GoBlog;

    let peers = [];
    let activePeerId = '';
    let wsBound = false;

    function pageEl() { return qs('dmPage'); }

    function getInitialPeerId() {
        const el = pageEl();
        return (el && el.getAttribute('data-peer-id')) || '';
    }

    function setText(id, text) {
        const el = qs(id);
        if (el) el.textContent = text;
    }

    function activePeer() {
        return String(activePeerId || '');
    }

    function renderPeers() {
        const root = qs('dmPeers');
        const empty = qs('dmPeersEmpty');
        if (!root || !empty) return;

        root.innerHTML = '';
        if (!peers.length) {
            empty.textContent = '暂无私信对象';
            empty.style.display = '';
            return;
        }
        empty.style.display = 'none';

        peers.forEach((p) => {
            const uid = String(p.user_id || p.userId || '');
            const name = String(p.user_name || p.userName || uid || '用户');
            const div = document.createElement('div');
            div.className = 'dm-peer' + (uid === activePeer() ? ' active' : '');

            const row = document.createElement('div');
            row.className = 'dm-peer-row';

            const left = document.createElement('div');
            left.className = 'dm-peer-name';
            left.textContent = name;

            // 右侧下拉：垃圾筐图标（删除会话）
            const actions = document.createElement('details');
            actions.className = 'dm-peer-actions';
            const sum = document.createElement('summary');
            sum.className = 'dm-peer-actions-summary';
            sum.setAttribute('aria-label', '更多');
            sum.textContent = '▾';

            // 打开下拉时不要触发“选中联系人/重新渲染”
            sum.addEventListener('click', function (ev) {
                ev.stopPropagation();
            });
            actions.addEventListener('click', function (ev) {
                ev.stopPropagation();
            });

            const menu = document.createElement('div');
            menu.className = 'dm-peer-actions-menu';
            const trash = document.createElement('button');
            trash.type = 'button';
            trash.className = 'dm-peer-trash-btn';
            trash.setAttribute('title', '删除与该用户的全部消息');
            trash.setAttribute('aria-label', '删除聊天');
            trash.textContent = '🗑';

            trash.addEventListener('click', async function (ev) {
                ev.preventDefault();
                ev.stopPropagation();
                if (!uid) return;
                if (!confirm('确定删除与该用户的全部消息吗？')) {
                    actions.open = false;
                    return;
                }
                try {
                    await api('/api/v1/dm/conversation/delete', {
                        method: 'POST',
                        body: JSON.stringify({ peerId: String(uid) })
                    });
                    actions.open = false;
                    // 如果删的是当前会话：清空右侧
                    if (activePeer() === String(uid)) {
                        activePeerId = '';
                        setText('dmChatTitle', '消息');
                        const msgRoot = qs('dmMessages');
                        if (msgRoot) msgRoot.innerHTML = '';
                        setText('dmMessagesEmpty', '请选择左侧消息对象');
                    }
                    await loadPeers();
                    if (activePeer()) {
                        await loadMessages();
                    }
                } catch (e) {
                    setText('dmMsg', '删除失败：' + (e && e.message || ''));
                }
            });

            menu.appendChild(trash);
            actions.appendChild(sum);
            actions.appendChild(menu);

            row.appendChild(left);
            row.appendChild(actions);

            const meta = document.createElement('div');
            meta.className = 'dm-peer-meta';
            meta.textContent = 'UID ' + uid;

            div.appendChild(row);
            div.appendChild(meta);

            div.addEventListener('click', function (ev) {
                // 点击下拉区域不选中联系人
                if (ev && ev.target && ev.target.closest && ev.target.closest('.dm-peer-actions')) {
                    return;
                }
                selectPeer(uid, name);
            });

            root.appendChild(div);
        });
    }

    function selectPeer(uid, name) {
        if (!uid) return;
        activePeerId = String(uid);
        setText('dmChatTitle', (name ? ('与 ' + name + ' 的消息') : '消息'));
        setText('dmMsg', '');
        renderPeers();
        loadMessages();
    }

    function renderMessages(list) {
        const root = qs('dmMessages');
        const empty = qs('dmMessagesEmpty');
        if (!root || !empty) return;

        root.innerHTML = '';
        if (!list || !list.length) {
            empty.textContent = '暂无消息';
            empty.style.display = '';
            return;
        }
        empty.style.display = 'none';

        const me = getAuth();
        const myId = String(me.userId || '');

        list.forEach((m) => {
            const from = String(m.fromUserId || m.from_user_id || '');
            const content = String(m.content || '')
                .replace(/\r\n/g, '\n')
                .replace(/\r/g, '\n')
                .replace(/\s+$/g, '');
            const sentAt = String(m.sentAt || m.sent_at || '');

            const isMe = (from && myId && from === myId);

            const row = document.createElement('div');
            // 需求：我的消息在左，对方消息在右
            row.className = 'dm-msg-row' + (isMe ? '' : ' other');

            const bubbleWrap = document.createElement('div');
            bubbleWrap.className = 'dm-bubble-wrap';

            const bubble = document.createElement('div');
            // 保持“我的消息”视觉区分（深色气泡），但方向按需求放左
            bubble.className = 'dm-bubble' + (isMe ? ' me' : '');
            bubble.textContent = content;

            const meta = document.createElement('div');
            meta.className = 'dm-meta';
            meta.textContent = sentAt;

            bubbleWrap.appendChild(bubble);
            if (sentAt) bubbleWrap.appendChild(meta);

            row.appendChild(bubbleWrap);
            root.appendChild(row);
        });

        // 滚动到底部
        try { root.scrollTop = root.scrollHeight; } catch { /* ignore */ }
    }

    async function loadPeers() {
        const empty = qs('dmPeersEmpty');
        if (empty) {
            empty.textContent = '加载中...';
            empty.style.display = '';
        }

        const resp = await api('/api/v1/dm/peers');
        const d = resp && resp.data;
        peers = (d && d.peers) || [];
        renderPeers();
    }

    async function loadMessages() {
        const pid = activePeer();
        const status = qs('dmStatus');
        if (!pid) return;

        if (status) status.textContent = '加载中...';
        try {
            const resp = await api('/api/v1/dm/messages?peer_id=' + encodeURIComponent(pid));
            const d = resp && resp.data;
            renderMessages((d && d.messages) || []);
            if (status) status.textContent = '';
        } catch (e) {
            if (status) status.textContent = '加载失败：' + (e && e.message || '');
        }
    }

    async function sendMessage(ev) {
        if (ev && typeof ev.preventDefault === 'function') {
            ev.preventDefault();
        }
        const pid = activePeer();
        const input = qs('dmInput');
        const msg = qs('dmMsg');
        const btn = qs('dmSendBtn');
        if (!pid || !input) return;

        const content = String(input.value || '').trim();
        if (!content) {
            if (msg) msg.textContent = '内容不能为空';
            return;
        }

        if (btn) btn.disabled = true;
        if (msg) msg.textContent = '发送中...';

        try {
            await api('/api/v1/dm/messages', {
                method: 'POST',
                body: JSON.stringify({ toUserId: String(pid), content })
            });
            input.value = '';
            if (msg) msg.textContent = '已发送';
            await loadPeers();
            await loadMessages();
        } catch (e) {
            if (msg) msg.textContent = '发送失败：' + (e && e.message || '');
        } finally {
            if (btn) btn.disabled = false;
        }
    }

    function bindWS() {
        if (wsBound) return;
        wsBound = true;

        window.addEventListener('goblog:ws-message', function (ev) {
            const raw = ev && ev.detail && ev.detail.data;
            if (!raw) return;

            let obj = null;
            try {
                obj = JSON.parse(raw);
            } catch {
                return;
            }

            if (!obj || !obj.type) return;

            // 登录后离线未读提示：自动拉取一次并清零
            if (obj.type === 'dm_unread') {
                const cnt = Number(obj.count || 0) || 0;
                if (cnt <= 0) return;
                loadPeers().then(function () {
                    if (activePeer()) return loadMessages();
                }).then(function () {
                    return api('/api/v1/dm/unread_clear', { method: 'POST' });
                }).then(function () {
                    try { localStorage.removeItem('gb_dm_hint'); } catch { /* ignore */ }
                }).catch(function () { });
                return;
            }

            if (obj.type !== 'dm') return;

            const me = getAuth();
            const myId = String(me.userId || '');
            const toId = String(obj.toUserId || obj.user_id || obj.userId || '');
            const fromId = String(obj.fromUserId || obj.sender_id || obj.senderId || '');

            // 只处理发给我的消息
            if (!myId || toId !== myId) return;

            // 刷新会话列表；若当前正在与该人聊天，则刷新消息
            loadPeers().catch(function () { });
            if (activePeer() && activePeer() === fromId) {
                loadMessages().catch(function () { });
            }
        });
    }

    document.addEventListener('DOMContentLoaded', async function () {
        const { token } = getAuth();
        if (!token) {
            setText('dmMessagesEmpty', '请先登录后再使用私信');
            const empty = qs('dmPeersEmpty');
            if (empty) empty.textContent = '';
            return;
        }

        try { ensureWSConnected(); } catch { /* ignore */ }
        bindWS();

        const form = qs('dmForm');
        if (form) form.addEventListener('submit', sendMessage);

        const input = qs('dmInput');
        if (input) {
            input.addEventListener('keydown', function (ev) {
                // Enter 发送；Shift+Enter 换行
                if (ev.key !== 'Enter') return;
                if (ev.shiftKey) return;
                // 中文输入法合成态：按回车选词时不发送
                if (ev.isComposing || ev.keyCode === 229) return;

                ev.preventDefault();
                sendMessage();
            });
        }

        await loadPeers();

        // 即使 WS connect 事件已在别的页面发生，这里也要兜底检查未读数
        try {
            const r = await api('/api/v1/dm/unread_count');
            const d = r && r.data;
            const cnt = Number((d && d.count) || 0) || 0;
            if (cnt > 0) {
                // 状态提示 + 先刷新会话列表
                setText('dmStatus', '未读 ' + cnt);
                await loadPeers();
            }
        } catch {
            // ignore
        }

        // 初始选择
        const initPeer = String(getInitialPeerId() || '');
        if (initPeer) {
            // 如果列表里没有该 peer，先临时塞一个占位
            const has = peers.some(p => String(p.user_id || p.userId || '') === initPeer);
            if (!has) {
                peers = [{ user_id: initPeer, user_name: '用户 ' + initPeer }].concat(peers);
                renderPeers();
                // 尝试补齐名字
                api('/api/v1/users/' + encodeURIComponent(initPeer)).then(function (resp) {
                    const d = resp && resp.data;
                    const nm = (d && (d.user_name || d.userName)) || ('用户 ' + initPeer);
                    peers = peers.map(function (p) {
                        const uid = String(p.user_id || p.userId || '');
                        if (uid !== initPeer) return p;
                        return { user_id: initPeer, user_name: nm };
                    });
                    renderPeers();
                }).catch(function () { });
            }
            const p = peers.find(p => String(p.user_id || p.userId || '') === initPeer);
            selectPeer(initPeer, p ? (p.user_name || p.userName || '') : '');
            return;
        }

        // 没有指定 peer：默认选第一个
        if (peers.length) {
            const p = peers[0];
            const uid = String(p.user_id || p.userId || '');
            const name = String(p.user_name || p.userName || uid);
            selectPeer(uid, name);
        }

        // 最后清一次未读（以 Redis 计数为准）
        api('/api/v1/dm/unread_clear', { method: 'POST' }).then(function () {
            try { localStorage.removeItem('gb_dm_hint'); } catch { /* ignore */ }
        }).catch(function () { });
    });
})();
