/* =========================================================================
   garden.anto.pt — interactions
   collection: live search + sortable columns (+ mobile sort select)
   ========================================================================= */
(function () {
  'use strict';

  var table = document.querySelector('table.data');
  if (!table) return;

  var tbody = table.tBodies[0];
  var rows = Array.prototype.slice.call(tbody.rows);
  var searchInput = document.querySelector('[data-search]');
  var countEl = document.querySelector('[data-count]');
  var countNoun = (countEl && countEl.getAttribute('data-noun')) || 'titles';
  var totalCount = rows.length;
  var sortSelect = document.querySelector('[data-sort-select]');

  var headers = table.tHead.rows[0].cells;
  var initialKey = 'title';
  var initialDir = 1;
  for (var h = 0; h < headers.length; h++) {
    var aria = headers[h].getAttribute('aria-sort');
    var key = headers[h].getAttribute('data-key');
    if (aria && key) {
      initialKey = key;
      initialDir = aria === 'descending' ? -1 : 1;
    }
  }

  var state = { q: '', key: initialKey, dir: initialDir };

  rows.forEach(function (tr) {
    for (var i = 0; i < tr.cells.length; i++) {
      var cell = tr.cells[i];
      var k = headers[i] && headers[i].getAttribute('data-key');
      if (!k) continue;
      var raw = cell.getAttribute('data-sort');
      if (raw === null) raw = cell.textContent;
      var num = parseFloat(raw);
      tr['_' + k] = isNaN(num) ? (raw || '').trim().toLowerCase() : num;
      if (k === 'title') tr._q = cell.textContent.trim().toLowerCase() + ' ' + (cell.getAttribute('data-extra') || '').toLowerCase();
    }
    if (tr._title === undefined) {
      tr._title = (tr.cells[0].getAttribute('data-sort') || tr.cells[0].textContent).trim().toLowerCase();
    }
    if (tr._q === undefined) {
      tr._q = tr.cells[0].textContent.trim().toLowerCase();
    }
  });

  function compare(a, b) {
    var av = a['_' + state.key];
    var bv = b['_' + state.key];
    if (state.key === 'title') {
      if (av < bv) return -1 * state.dir;
      if (av > bv) return 1 * state.dir;
      return 0;
    }
    var avNum = typeof av === 'number' ? av : null;
    var bvNum = typeof bv === 'number' ? bv : null;
    if (avNum === null && bvNum === null) return a._title < b._title ? -1 : 1;
    if (avNum === null) return 1;
    if (bvNum === null) return -1;
    if (avNum === bvNum) return a._title < b._title ? -1 : 1;
    return (avNum - bvNum) * state.dir;
  }

  function render() {
    var q = state.q;
    var visible = rows.filter(function (tr) {
      return !q || tr._q.indexOf(q) !== -1;
    });
    visible.sort(compare);

    var frag = document.createDocumentFragment();
    visible.forEach(function (tr) { tr.style.display = ''; frag.appendChild(tr); });
    rows.forEach(function (tr) {
      if (visible.indexOf(tr) === -1) tr.style.display = 'none';
    });
    tbody.appendChild(frag);
    var shown = visible.length;

    if (countEl) {
      if (q) countEl.innerHTML = '<b>' + shown + '</b> of ' + totalCount;
      else countEl.innerHTML = '<b>' + totalCount + '</b> ' + countNoun;
    }

    var es = document.querySelector('[data-empty]');
    if (es) es.hidden = shown !== 0;
    table.hidden = shown === 0;

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
