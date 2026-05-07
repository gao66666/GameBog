(function () {
  'use strict';

  function qs(id) { return document.getElementById(id); }

  function coverSrc(g) {
    var u = String((g && (g.coverUrl || g.cover_url)) || '').trim();
    if (/^https?:\/\//i.test(u)) return u;
    return (window.GameBog && window.GameBog.defaultGameCoverUrl) ||
      'https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg';
  }

  function formatPrice(g) {
    var pc = g.priceCents != null ? g.priceCents : g.price_cents;
    if (pc === undefined || pc === null) pc = -1;
    pc = Number(pc);
    if (pc < 0) return '价格待定（见各商店）';
    if (pc === 0) return '免费';
    var yuan = pc / 100;
    if (Math.abs(yuan - Math.round(yuan)) < 1e-6) return '¥' + Math.round(yuan);
    return '¥' + yuan.toFixed(2);
  }

  function formatTags(g) {
    var raw = g.tags;
    var out = [];
    if (Array.isArray(raw)) {
      raw.forEach(function (t) {
        var s = String(t || '').trim();
        if (s) out.push(s);
      });
    } else if (raw && typeof raw === 'string') {
      try {
        var j = JSON.parse(raw);
        if (Array.isArray(j)) {
          j.forEach(function (t) {
            var s = String(t || '').trim();
            if (s) out.push(s);
          });
        }
      } catch (e) { /* ignore */ }
    }
    if (out.length) return out.join(' · ');
    return '—';
  }

  function fmtDate(ra) {
    if (!ra) return '—';
    var d = typeof ra === 'string' ? new Date(ra) : new Date(ra);
    if (isNaN(d.getTime())) return '—';
    return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
  }

  function fmtDateTime(iso) {
    if (!iso) return '';
    var d = typeof iso === 'string' ? new Date(iso) : new Date(iso);
    if (isNaN(d.getTime())) return '';
    return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0') +
      ' ' + String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
  }

  function starsText(n) {
    var x = Math.max(1, Math.min(5, Number(n) || 0));
    var s = '';
    var i;
    for (i = 0; i < x; i++) s += '★';
    for (; i < 5; i++) s += '☆';
    return s;
  }

  function getAuth() {
    if (window.GoBlog && window.GoBlog.getAuth) return window.GoBlog.getAuth();
    return { token: '', userId: '', userName: '' };
  }

  function uidStr() {
    var a = getAuth();
    return String(a.userId || '').trim();
  }

  document.addEventListener('DOMContentLoaded', function () {
    if (window.GoBlog && window.GoBlog.setupNav) window.GoBlog.setupNav();

    var id = qs('glGameId');
    if (!id || !id.value) return;
    var gid = id.value.trim();

    var titleEl = qs('glDetailTitle');
    var subEl = qs('glDetailSub');
    var cover = qs('glDetailCover');
    var desc = qs('glDetailDesc');
    var pub = qs('glDetailPub');
    var dev = qs('glDetailDev');
    var date = qs('glDetailDate');
    var priceEl = qs('glDetailPrice');
    var tagsEl = qs('glDetailTags');

    var reviewsList = qs('glReviewsList');
    var reviewsEmpty = qs('glReviewsEmpty');
    var reviewsHint = qs('glReviewsLoginHint');
    var composer = qs('glReviewComposer');
    var ratingSel = qs('glReviewRating');
    var contentTa = qs('glReviewContent');
    var submitBtn = qs('glReviewSubmit');
    var cancelBtn = qs('glReviewCancel');
    var reviewMsg = qs('glReviewMsg');

    var editingId = null;

    function setComposerMode(mode, preset) {
      editingId = mode === 'edit' && preset && preset.id ? String(preset.id) : null;
      if (ratingSel) ratingSel.value = String((preset && preset.rating) || 5);
      if (contentTa) contentTa.value = (preset && preset.content) ? String(preset.content) : '';
      if (submitBtn) submitBtn.textContent = editingId ? '保存修改' : '发表点评';
      if (cancelBtn) cancelBtn.style.display = editingId ? 'inline-flex' : 'none';
    }

    function showComposerForLogin() {
      var tok = getAuth().token;
      if (reviewsHint) reviewsHint.style.display = tok ? 'none' : '';
      if (!composer) return;
      if (!tok) {
        composer.style.display = 'none';
        return;
      }
      var mine = findMyReview();
      if (mine && !editingId) {
        composer.style.display = 'none';
        return;
      }
      composer.style.display = '';
      if (!editingId && !mine) setComposerMode('create', null);
    }

    var cachedReviews = [];

    function findMyReview() {
      var me = uidStr();
      if (!me) return null;
      var i;
      for (i = 0; i < cachedReviews.length; i++) {
        var r = cachedReviews[i];
        if (!r) continue;
        var uid = String(r.userId != null ? r.userId : r.user_id || '').trim();
        if (uid === me) return r;
      }
      return null;
    }

    function renderReviews(list) {
      cachedReviews = Array.isArray(list) ? list : [];
      if (!reviewsList) return;
      reviewsList.innerHTML = '';
      var me = uidStr();
      if (!cachedReviews.length) {
        if (reviewsEmpty) reviewsEmpty.style.display = '';
        showComposerForLogin();
        return;
      }
      if (reviewsEmpty) reviewsEmpty.style.display = 'none';

      cachedReviews.forEach(function (r) {
        var rid = String(r.id != null ? r.id : '');
        var uid = String(r.userId != null ? r.userId : r.user_id || '').trim();
        var uname = String(r.userName || r.user_name || '玩家').trim() || '玩家';
        var row = document.createElement('article');
        row.className = 'gd-review-item';
        row.setAttribute('data-id', rid);

        var hd = document.createElement('div');
        hd.className = 'gd-review-item-hd';
        var nameSp = document.createElement('span');
        nameSp.className = 'gd-review-user';
        nameSp.textContent = uname;
        var starSp = document.createElement('span');
        starSp.className = 'gd-review-stars';
        starSp.textContent = starsText(r.rating);
        var timeSp = document.createElement('span');
        timeSp.className = 'gd-review-time';
        timeSp.textContent = fmtDateTime(r.reviewedAt || r.reviewed_at);
        hd.appendChild(nameSp);
        hd.appendChild(starSp);
        hd.appendChild(timeSp);
        row.appendChild(hd);

        var body = document.createElement('div');
        body.className = 'gd-review-body';
        body.textContent = String(r.content || '').trim();
        row.appendChild(body);

        if (me && uid === me) {
          var act = document.createElement('div');
          act.className = 'gd-review-own';
          var btnEd = document.createElement('button');
          btnEd.type = 'button';
          btnEd.className = 'gd-review-linkbtn';
          btnEd.textContent = '编辑';
          btnEd.addEventListener('click', function () {
            setComposerMode('edit', { id: rid, rating: r.rating, content: r.content });
            if (composer) composer.style.display = '';
            if (composer) composer.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
          });
          var btnDel = document.createElement('button');
          btnDel.type = 'button';
          btnDel.className = 'gd-review-linkbtn danger';
          btnDel.textContent = '删除';
          btnDel.addEventListener('click', function () {
            if (!confirm('确定删除这条点评？')) return;
            deleteReviewApi(rid);
          });
          act.appendChild(btnEd);
          act.appendChild(btnDel);
          row.appendChild(act);
        }

        reviewsList.appendChild(row);
      });

      showComposerForLogin();
    }

    async function loadReviews() {
      if (!window.GoBlog || !window.GoBlog.api) return;
      try {
        var body = await window.GoBlog.api('/api/v1/games/' + encodeURIComponent(gid) + '/reviews?page=1&size=50');
        var d = body.data || body;
        var list = (d && d.list) || [];
        renderReviews(list);
      } catch (e) {
        if (reviewsList) {
          reviewsList.innerHTML = '<p class="gd-review-err">点评加载失败：' + (e && e.message ? e.message : e) + '</p>';
        }
      }
    }

    function setReviewMsg(t, isErr) {
      if (!reviewMsg) return;
      reviewMsg.textContent = t || '';
      reviewMsg.style.color = isErr ? '#f87171' : '#6ee7b7';
    }

    async function submitReview() {
      if (!window.GoBlog || !window.GoBlog.api) return;
      var tok = getAuth().token;
      if (!tok) {
        setReviewMsg('请先登录', true);
        return;
      }
      var rating = ratingSel ? parseInt(ratingSel.value, 10) : 5;
      var content = contentTa ? String(contentTa.value || '').trim() : '';
      if (!content) {
        setReviewMsg('请填写点评内容', true);
        return;
      }
      setReviewMsg('提交中…', false);
      if (submitBtn) submitBtn.disabled = true;
      try {
        if (editingId) {
          await window.GoBlog.api('/api/v1/game-reviews/' + encodeURIComponent(editingId), {
            method: 'PUT',
            body: JSON.stringify({ rating: rating, content: content })
          });
          setReviewMsg('已保存', false);
        } else {
          await window.GoBlog.api('/api/v1/games/' + encodeURIComponent(gid) + '/reviews', {
            method: 'POST',
            body: JSON.stringify({ rating: rating, content: content })
          });
          setReviewMsg('发表成功', false);
        }
        setComposerMode('create', null);
        await loadReviews();
      } catch (e) {
        setReviewMsg((e && e.message) || String(e), true);
      } finally {
        if (submitBtn) submitBtn.disabled = false;
      }
    }

    async function deleteReviewApi(rid) {
      if (!window.GoBlog || !window.GoBlog.api) return;
      setReviewMsg('', false);
      try {
        await window.GoBlog.api('/api/v1/game-reviews/' + encodeURIComponent(rid), { method: 'DELETE' });
        await loadReviews();
      } catch (e) {
        alert((e && e.message) || String(e));
      }
    }

    if (window.GoBlog && window.GoBlog.api) {
      window.GoBlog.api('/api/v1/games/' + encodeURIComponent(gid))
        .then(function (body) {
          var d = body.data || body;
          var g = d.game;
          if (!g) throw new Error('无 game 字段');
          if (titleEl) titleEl.textContent = g.name || '';
          if (subEl) subEl.textContent = (g.publisher || '') + (g.developer ? ' · ' + g.developer : '');
          if (cover) {
            cover.src = coverSrc(g);
            cover.alt = g.name || '';
          }
          if (desc) desc.textContent = g.description || '暂无简介';
          if (priceEl) priceEl.textContent = formatPrice(g);
          if (tagsEl) tagsEl.textContent = formatTags(g);
          if (pub) pub.textContent = g.publisher || '—';
          if (dev) dev.textContent = g.developer || '—';
          if (date) date.textContent = fmtDate(g.releaseAt);
        })
        .then(function () { return loadReviews(); })
        .catch(function (e) {
          if (titleEl) titleEl.textContent = '加载失败';
          if (desc) desc.textContent = (e && e.message) || String(e);
        });
    }

    if (submitBtn) submitBtn.addEventListener('click', function () { submitReview(); });
    if (cancelBtn) {
      cancelBtn.addEventListener('click', function () {
        editingId = null;
        setComposerMode('create', null);
        showComposerForLogin();
        setReviewMsg('', false);
      });
    }
  });
})();
