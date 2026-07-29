/* =========================================================================
   garden.anto.pt — interactions
   collection: live search + sortable columns (+ mobile sort select)
   drives either a table.data or a [data-grid] of cards
   ========================================================================= */
(function () {
  'use strict';

  var GRID_KEYS = ['title', 'rating', 'imdb', 'added'];

  // some covers have rotted upstream; drop the broken img so the title shows through
  function dropBroken(img) {
    if (img && img.tagName === 'IMG' && img.closest('.poster-card')) img.remove();
  }
  window.addEventListener('error', function (e) { dropBroken(e.target); }, true);
  window.addEventListener('load', function () {
    Array.prototype.forEach.call(document.querySelectorAll('.poster-card img'), function (img) {
      if (img.complete && img.naturalWidth === 0) dropBroken(img);
    });
  });

  var table = document.querySelector('table.data');
  var grid = document.querySelector('[data-grid]');
  var container = table ? table.tBodies[0] : grid;
  if (!container) return;

  var items = Array.prototype.slice.call(table ? container.rows : container.children);
  var headers = table ? table.tHead.rows[0].cells : [];
  var searchInput = document.querySelector('[data-search]');
  var countEl = document.querySelector('[data-count]');
  var countNoun = (countEl && countEl.getAttribute('data-noun')) || 'titles';
  var totalCount = items.length;
  var sortSelect = document.querySelector('[data-sort-select]');
  var genreSelect = document.querySelector('[data-genre-select]');
  var flagToggle = document.querySelector('[data-flag-filter]');
  var flagAttr = flagToggle ? 'data-' + flagToggle.getAttribute('data-flag-filter') : null;

  var initialKey = 'title';
  var initialDir = 1;
  if (table) {
    for (var h = 0; h < headers.length; h++) {
      var aria = headers[h].getAttribute('aria-sort');
      var key = headers[h].getAttribute('data-key');
      if (aria && key) {
        initialKey = key;
        initialDir = aria === 'descending' ? -1 : 1;
      }
    }
  } else if (sortSelect) {
    var initial = sortSelect.value.split(':');
    initialKey = initial[0];
    initialDir = initial[1] === 'asc' ? 1 : -1;
  }

  var state = { q: '', key: initialKey, dir: initialDir, flag: false, genre: '' };

  // titles are never numeric: parseFloat('12th Fail') is 12, which would make
  // every digit-leading title compare equal to the string ones and never sort
  function val(raw, key) {
    raw = (raw || '').trim();
    if (key === 'title') return raw.toLowerCase();
    var n = parseFloat(raw);
    return isNaN(n) ? raw.toLowerCase() : n;
  }

  items.forEach(function (el) {
    el._flag = flagAttr ? el.hasAttribute(flagAttr) : false;
    el._genres = (el.getAttribute('data-genres') || '').toLowerCase();

    if (!table) {
      GRID_KEYS.forEach(function (k) { el['_' + k] = val(el.getAttribute('data-sort-' + k), k); });
      el._q = (el.getAttribute('data-q') || el.textContent).trim().toLowerCase();
      return;
    }

    for (var i = 0; i < el.cells.length; i++) {
      var cell = el.cells[i];
      var k = headers[i] && headers[i].getAttribute('data-key');
      if (!k) continue;
      var raw = cell.getAttribute('data-sort');
      if (raw === null) raw = cell.textContent;
      el['_' + k] = val(raw, k);
      if (k === 'title') el._q = cell.textContent.trim().toLowerCase() + ' ' + (cell.getAttribute('data-extra') || '').toLowerCase();
    }
    if (el._title === undefined) {
      el._title = val(el.cells[0].getAttribute('data-sort') || el.cells[0].textContent, 'title');
    }
    if (el._q === undefined) {
      el._q = el.cells[0].textContent.trim().toLowerCase();
    }
  });

  function byTitle(a, b) { return a._title.localeCompare(b._title); }

  function compare(a, b) {
    var av = a['_' + state.key];
    var bv = b['_' + state.key];
    if (state.key === 'title') return byTitle(a, b) * state.dir;
    var avNum = typeof av === 'number' ? av : null;
    var bvNum = typeof bv === 'number' ? bv : null;
    if (avNum === null && bvNum === null) return byTitle(a, b);
    if (avNum === null) return 1;
    if (bvNum === null) return -1;
    if (avNum === bvNum) return byTitle(a, b);
    return (avNum - bvNum) * state.dir;
  }

  function render() {
    var q = state.q;
    var genre = state.genre;
    var visible = items.filter(function (el) {
      if (state.flag && !el._flag) return false;
      if (genre && el._genres.indexOf('|' + genre + '|') === -1) return false;
      return !q || el._q.indexOf(q) !== -1;
    });
    visible.sort(compare);

    var frag = document.createDocumentFragment();
    visible.forEach(function (el) { el.style.display = ''; frag.appendChild(el); });
    items.forEach(function (el) {
      if (visible.indexOf(el) === -1) el.style.display = 'none';
    });
    container.appendChild(frag);
    var shown = visible.length;

    if (countEl) {
      if (q || state.flag || genre) countEl.innerHTML = '<b>' + shown + '</b> of ' + totalCount;
      else countEl.innerHTML = '<b>' + totalCount + '</b> ' + countNoun;
    }

    var es = document.querySelector('[data-empty]');
    if (es) es.hidden = shown !== 0;
    (table || grid).hidden = shown === 0;

    for (var i = 0; i < headers.length; i++) {
      var key = headers[i].getAttribute('data-key');
      headers[i].removeAttribute('aria-sort');
      var caret = headers[i].querySelector('.sortcaret');
      if (!caret) continue;
      if (key === state.key) {
        headers[i].setAttribute('aria-sort', state.dir === 1 ? 'ascending' : 'descending');
        caret.textContent = state.dir === 1 ? '▲' : '▼';
      } else {
        caret.textContent = '↕';
      }
    }
  }

  function setSort(key) {
    if (state.key === key) {
      state.dir *= -1;
    } else {
      state.key = key;
      state.dir = (key === 'title') ? 1 : -1;
    }
    if (sortSelect) sortSelect.value = state.key + ':' + (state.dir === 1 ? 'asc' : 'desc');
    render();
  }

  Array.prototype.forEach.call(headers, function (th) {
    var key = th.getAttribute('data-key');
    if (!key) return;
    th.addEventListener('click', function () { setSort(key); });
  });

  if (searchInput) {
    searchInput.addEventListener('input', function () {
      state.q = searchInput.value.trim().toLowerCase();
      render();
    });
  }

  if (flagToggle) {
    flagToggle.addEventListener('change', function () {
      state.flag = flagToggle.checked;
      render();
    });
  }

  if (genreSelect) {
    genreSelect.addEventListener('change', function () {
      state.genre = genreSelect.value;
      render();
    });
  }

  if (sortSelect) {
    sortSelect.addEventListener('change', function () {
      var parts = sortSelect.value.split(':');
      state.key = parts[0];
      state.dir = parts[1] === 'asc' ? 1 : -1;
      render();
    });
  }

  document.addEventListener('click', function (e) {
    var loc = e.target.closest && e.target.closest('.loc');
    if (!loc || !searchInput) return;
    e.preventDefault();
    searchInput.value = loc.textContent.trim();
    state.q = searchInput.value.trim().toLowerCase();
    render();
    searchInput.focus();
  });

  render();
})();
