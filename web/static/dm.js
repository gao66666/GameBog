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
            div.className = 'item row';
            div.style.cursor = 'pointer';
            div.style.justifyContent = 'space-between';
            div.style.alignItems = 'center';

            const left = document.createElement('div');
            left.textContent = name;

            const right = document.createElement('div');
            right.className = 'muted';
            right.textContent = uid === activePeer() ? '当前' : '';

            div.appendChild(left);
            div.appendChild(right);

            div.onclick = function () {
                selectPeer(uid, name);
            };

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
            const content = String(m.content || '');
            const sentAt = String(m.sentAt || m.sent_at || '');

            const box = document.createElement('div');
            box.className = 'item';
            box.style.textAlign = (from && myId && from === myId) ? 'right' : 'left';

            const line = document.createElement('div');
            line.textContent = content;

            const meta = document.createElement('div');
            meta.className = 'muted';
            meta.textContent = sentAt;

            box.appendChild(line);
            if (sentAt) box.appendChild(meta);
            root.appendChild(box);
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
        ev.preventDefault();
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

            if (!obj || obj.type !== 'dm') return;

            const me = getAuth();
            const myId = String(me.userId || '');
            const toId = String(obj.toUserId || '');
            const fromId = String(obj.fromUserId || '');

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

        await loadPeers();

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
    });
})();
