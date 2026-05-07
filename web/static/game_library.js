/**
 * 游戏库页：从 /api/v1/games 拉取列表，渲染精选轮播与下方目录。
 */
(function () {
  'use strict';

  function tagEl(t) {
    var s = document.createElement('span');
    s.className = 'gl-tag';
    s.textContent = t;
    return s;
  }

  /** 仅当后台填写合法 HTTPS 封面时使用；否则统一默认图（与数据库空字符串一致，不再用 Picsum 假随机） */
  function coverSrc(g) {
    var u = String((g && (g.coverUrl || g.cover_url)) || '').trim();
    if (/^https?:\/\//i.test(u)) return u;
    return (window.GameBog && window.GameBog.defaultGameCoverUrl) ||
      'https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg';
  }

  function priceCentsOf(g) {
    var pc = g.priceCents != null ? g.priceCents : g.price_cents;
    if (pc === undefined || pc === null) return -1;
    var n = Number(pc);
    return isNaN(n) ? -1 : n;
  }

  /** 列表与卡片展示用 */
  function formatPrice(g) {
    var pc = priceCentsOf(g);
    if (pc < 0) return '价格待定';
    if (pc === 0) return '免费';
    var yuan = pc / 100;
    if (Math.abs(yuan - Math.round(yuan)) < 1e-6) return '¥' + Math.round(yuan);
    return '¥' + yuan.toFixed(2);
  }

  function collectTags(g, maxN) {
    maxN = maxN || 4;
    var out = [];
    var raw = g.tags;
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
    if (out.length) return out.slice(0, maxN);
    return [g.publisher, g.developer].filter(Boolean).slice(0, maxN);
  }

  /** stripe: 'long' | 'short' | null */
  function cardNode(g, stripe, href) {
    var card = document.createElement(href ? 'a' : 'article');
    if (href) {
      card.href = href;
      card.style.textDecoration = 'none';
      card.style.color = 'inherit';
      card.style.display = 'block';
    }
    if (stripe === 'long') {
      card.className = 'gl-stripe-card gl-stripe-long';
    } else if (stripe === 'short') {
      card.className = 'gl-stripe-card gl-stripe-short';
    } else {
      card.className = 'gl-card';
    }
    var img = document.createElement('img');
    img.className = 'gl-cover';
    img.src = coverSrc(g);
    img.alt = g.name || '';
    img.loading = 'lazy';
    card.appendChild(img);
    var body = document.createElement('div');
    if (stripe === 'long' || stripe === 'short') {
      body.className = 'gl-stripe-body' + (stripe === 'short' ? ' is-compact' : '');
    } else {
      body.className = 'gl-card-body';
    }
    var h = document.createElement('div');
    if (stripe === 'long') h.className = 'gl-stripe-title';
    else if (stripe === 'short') h.className = 'gl-stripe-title is-sm';
    else h.className = 'gl-card-title';
    h.textContent = g.name || '';
    body.appendChild(h);
    var prow = document.createElement('div');
    prow.className = 'gl-price-row';
    var price = document.createElement('span');
    var pc = priceCentsOf(g);
    if (pc < 0) price.className = 'gl-price is-unknown';
    else if (pc === 0) price.className = 'gl-price is-free';
    else price.className = 'gl-price is-paid';
    price.textContent = formatPrice(g);
    prow.appendChild(price);
    body.appendChild(prow);
    var tags = document.createElement('div');
    tags.className = 'gl-tags';
    collectTags(g, stripe === 'short' ? 2 : 4).forEach(function (t) {
      tags.appendChild(tagEl(String(t).trim()));
    });
    body.appendChild(tags);
    card.appendChild(body);
    return card;
  }

  var carPos = 0;
  var slideW = 0;
  var halfAW = 0;
  var GAMES = [];

  function layoutCarousel() {
    var vp = document.getElementById('glCarViewport');
    var halfA = document.getElementById('glCarHalfA');
    var track = document.getElementById('glCarTrack');
    if (!vp || !halfA || !track) return;
    slideW = vp.clientWidth;
    var slides = halfA.querySelectorAll('.gl-car-slide');
    slides.forEach(function (sl) {
      sl.style.width = slideW + 'px';
      sl.style.minWidth = slideW + 'px';
      sl.style.maxWidth = slideW + 'px';
    });
    halfAW = halfA.offsetWidth;
    track.classList.add('no-anim');
    track.style.transform = 'translate3d(' + (-carPos) + 'px,0,0)';
    void track.offsetHeight;
    track.classList.remove('no-anim');
  }

  function fillFeaturedCarousel() {
    var halfA = document.getElementById('glCarHalfA');
    var halfB = document.getElementById('glCarHalfB');
    var track = document.getElementById('glCarTrack');
    if (!halfA || !halfB || !track) return;
    if (!GAMES.length) {
      halfA.innerHTML = '<p style="padding:24px;color:#8b919a">暂无游戏数据，请运行 <code>go run ./cmd/seedgames</code> 导入种子。</p>';
      return;
    }
    halfA.innerHTML = '';
    var slideCount = Math.min(5, GAMES.length);
    for (var s = 0; s < slideCount; s++) {
      var slide = document.createElement('div');
      slide.className = 'gl-car-slide';
      var g = GAMES[s % GAMES.length];
      slide.appendChild(cardNode(g, 'long', '/game/' + encodeURIComponent(g.id)));
      halfA.appendChild(slide);
    }
    halfB.innerHTML = halfA.innerHTML;
    carPos = 0;
    layoutCarousel();

    function nudge(dir) {
      var vp = document.getElementById('glCarViewport');
      var tr = document.getElementById('glCarTrack');
      if (!vp || !tr) return;
      slideW = vp.clientWidth;
      halfAW = document.getElementById('glCarHalfA').offsetWidth;
      carPos += dir * slideW;
      if (carPos >= halfAW - 1) {
        tr.classList.add('no-anim');
        carPos -= halfAW;
        tr.style.transform = 'translate3d(' + (-carPos) + 'px,0,0)';
        void tr.offsetHeight;
        tr.classList.remove('no-anim');
      } else if (carPos < 0) {
        tr.classList.add('no-anim');
        carPos += halfAW;
        tr.style.transform = 'translate3d(' + (-carPos) + 'px,0,0)';
        void tr.offsetHeight;
        tr.classList.remove('no-anim');
      }
      tr.style.transform = 'translate3d(' + (-carPos) + 'px,0,0)';
    }

    var prev = document.getElementById('glFeatPrev');
    var next = document.getElementById('glFeatNext');
    if (prev) prev.onclick = function () { nudge(-1); };
    if (next) next.onclick = function () { nudge(1); };
    window.addEventListener('resize', function () { layoutCarousel(); });
  }

  var catalogIx = 0;
  function takeOne() {
    if (!GAMES.length) return null;
    return GAMES[catalogIx++ % GAMES.length];
  }
  function takeN(n) {
    var arr = [];
    for (var i = 0; i < n; i++) {
      var g = takeOne();
      if (g) arr.push(g);
    }
    return arr;
  }

  function buildCatalogInto(root) {
    if (!root) return;
    root.innerHTML = '';
    if (!GAMES.length) {
      root.innerHTML = '<p style="padding:16px;color:#8b919a">暂无数据</p>';
      return;
    }
    var cycles = Math.max(1, Math.ceil(18 / GAMES.length));
    for (var c = 0; c < Math.min(3, cycles); c++) {
      var heroBlock = document.createElement('section');
      heroBlock.className = 'gl-catalog-block';
      var labH = document.createElement('p');
      labH.className = 'gl-catalog-label';
      labH.textContent = '焦点推荐';
      heroBlock.appendChild(labH);
      var heroInner = document.createElement('div');
      heroInner.className = 'gl-catalog-hero-inner';
      var a = takeOne();
      var b = takeOne();
      if (a) heroInner.appendChild(cardNode(a, 'long', '/game/' + encodeURIComponent(a.id)));
      if (b) heroInner.appendChild(cardNode(b, 'long', '/game/' + encodeURIComponent(b.id)));
      heroBlock.appendChild(heroInner);
      root.appendChild(heroBlock);

      var gridBlock = document.createElement('section');
      gridBlock.className = 'gl-catalog-block';
      var labG = document.createElement('p');
      labG.className = 'gl-catalog-label';
      labG.textContent = '更多游戏';
      gridBlock.appendChild(labG);
      var gw = document.createElement('div');
      gw.className = 'gl-catalog-grid-wrap';
      var grid = document.createElement('div');
      grid.className = 'gl-grid';
      takeN(9).forEach(function (g) {
        grid.appendChild(cardNode(g, null, '/game/' + encodeURIComponent(g.id)));
      });
      gw.appendChild(grid);
      gridBlock.appendChild(gw);
      root.appendChild(gridBlock);
    }
  }

  function setupCatalogLoop() {
    var vp = document.getElementById('glLoopViewport');
    var A = document.getElementById('glLoopA');
    var B = document.getElementById('glLoopB');
    if (!vp || !A || !B) return;
    catalogIx = 0;
    buildCatalogInto(A);
    B.innerHTML = A.innerHTML;

    var adjusting = false;
    vp.addEventListener('scroll', function () {
      if (adjusting) return;
      var h = A.offsetHeight;
      if (h < 40) return;
      var st = vp.scrollTop;
      if (st >= h - 2) {
        adjusting = true;
        vp.scrollTop = st - h;
        adjusting = false;
      } else if (st <= 2) {
        adjusting = true;
        vp.scrollTop = st + h;
        adjusting = false;
      }
    }, { passive: true });
  }

  async function loadGames() {
    var api = window.GoBlog && window.GoBlog.api;
    if (!api) throw new Error('GoBlog.api missing');
    var body = await api('/api/v1/games?page=1&size=200');
    var d = body.data || body;
    GAMES = (d && d.list) || [];
  }

  document.addEventListener('DOMContentLoaded', function () {
    if (window.GoBlog && window.GoBlog.setupNav) window.GoBlog.setupNav();
    loadGames()
      .then(function () {
        fillFeaturedCarousel();
        setupCatalogLoop();
      })
      .catch(function (e) {
        console.error(e);
        var halfA = document.getElementById('glCarHalfA');
        if (halfA) {
          halfA.innerHTML = '<p style="padding:24px;color:#f87171">加载失败：' + (e && e.message ? e.message : e) + '</p>';
        }
      });
  });
})();
