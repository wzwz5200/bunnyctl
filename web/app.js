'use strict';

/* ══════════════════════════════════════════════════════════════
   state
   ══════════════════════════════════════════════════════════════ */
const FMIN = 20, FMAX = 20000, GMIN = -15, GMAX = 15;
const TYPE_LABEL = { PK: '峰值', LSH: '低搁架', HSH: '高搁架', LPF: '低通', HPF: '高通' };
const BAND_COLORS = ['#7ED8BE', '#A8C7FA', '#F2C46B', '#FFB4AB', '#D0BCFF'];
const C = {
  grid: 'rgba(137,147,141,.13)',
  gridStrong: 'rgba(137,147,141,.30)',
  axis: 'rgba(191,201,195,.62)',
  curve: '#7ED8BE',
  curveSoft: 'rgba(137,147,141,.35)',
  canvas: '#090F0D',
};

const state = {
  bank: 'dac',
  live: null,          // state currently in device RAM
  draft: null,         // what the UI shows
  enabled: true,
  preamp: 0,
  savedPreamp: 0,
  unsaved: false,
  connected: false,
  info: null,
  presets: [],
  drag: -1,
  hover: -1,
  fs: 48000,
  applying: false,
};

const $ = (id) => document.getElementById(id);

function updateFill(el) {
  const min = parseFloat(el.min), max = parseFloat(el.max), v = parseFloat(el.value);
  el.style.setProperty('--fill', ((v - min) / (max - min) * 100).toFixed(2) + '%');
}

/* MD3 state-layer ripple for every button-like control */
function attachRipple() {
  document.addEventListener('pointerdown', (ev) => {
    const el = ev.target.closest && ev.target.closest('.btn, .chip, .seg-btn');
    if (!el || el.disabled) return;
    const r = el.getBoundingClientRect();
    const d = Math.max(r.width, r.height) * 2;
    const span = document.createElement('span');
    span.className = 'ripple';
    span.style.width = span.style.height = d + 'px';
    span.style.left = (ev.clientX - r.left - d / 2) + 'px';
    span.style.top = (ev.clientY - r.top - d / 2) + 'px';
    el.appendChild(span);
    setTimeout(() => span.remove(), 520);
  }, { passive: true });
}

/* ══════════════════════════════════════════════════════════════
   api
   ══════════════════════════════════════════════════════════════ */
async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* ignore */ }
  if (!res.ok) {
    const err = new Error((data && data.error) || `HTTP ${res.status}`);
    err.status = res.status;
    throw err;
  }
  return data;
}

let toastTimer = null;
function toast(msg, kind = '') {
  const el = $('toast');
  el.textContent = msg;
  el.className = 'snackbar show ' + kind;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = 'snackbar ' + kind; }, 2600);
}

/* ══════════════════════════════════════════════════════════════
   biquad math (RBJ cookbook, matches the device's encoding)
   ══════════════════════════════════════════════════════════════ */
function biquad(b, fs) {
  const f = Math.min(Math.max(b.freq, 1), fs / 2 * 0.99);
  const gain = b.gain, q = Math.max(b.q, 0.01);
  const A = Math.pow(10, gain / 40);
  const w0 = 2 * Math.PI * f / fs;
  const cw = Math.cos(w0), sw = Math.sin(w0);
  let b0, b1, b2, a0, a1, a2;

  if (b.type === 'LPF' || b.type === 'HPF') {
    const alpha = sw / (2 * q);
    if (b.type === 'LPF') {
      b0 = (1 - cw) / 2; b1 = 1 - cw; b2 = (1 - cw) / 2;
    } else {
      b0 = (1 + cw) / 2; b1 = -(1 + cw); b2 = (1 + cw) / 2;
    }
    a0 = 1 + alpha; a1 = -2 * cw; a2 = 1 - alpha;
  } else if (b.type === 'LSH' || b.type === 'HSH') {
    const alpha = sw / 2 * Math.SQRT2;
    const two = 2 * Math.sqrt(A) * alpha;
    if (b.type === 'LSH') {
      b0 = A * ((A + 1) - (A - 1) * cw + two);
      b1 = 2 * A * ((A - 1) - (A + 1) * cw);
      b2 = A * ((A + 1) - (A - 1) * cw - two);
      a0 = (A + 1) + (A - 1) * cw + two;
      a1 = -2 * ((A - 1) + (A + 1) * cw);
      a2 = (A + 1) + (A - 1) * cw - two;
    } else {
      b0 = A * ((A + 1) + (A - 1) * cw + two);
      b1 = -2 * A * ((A - 1) + (A + 1) * cw);
      b2 = A * ((A + 1) + (A - 1) * cw - two);
      a0 = (A + 1) - (A - 1) * cw + two;
      a1 = 2 * ((A - 1) - (A + 1) * cw);
      a2 = (A + 1) - (A - 1) * cw - two;
    }
  } else { // PK
    const alpha = sw / (2 * q);
    b0 = 1 + alpha * A; b1 = -2 * cw; b2 = 1 - alpha * A;
    a0 = 1 + alpha / A; a1 = -2 * cw; a2 = 1 - alpha / A;
  }
  return [b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0];
}

function responseAt(bands, f, fs) {
  let mag = 1;
  const cw = Math.cos(2 * Math.PI * f / fs), sw = Math.sin(2 * Math.PI * f / fs);
  // z^-1 = e^{-jw}
  const z1r = cw, z1i = -sw;
  const z2r = cw * cw - sw * sw, z2i = -2 * cw * sw;
  for (const b of bands) {
    if (!b || !b.freq) continue;
    const [b0, b1, b2, a1, a2] = biquad(b, fs);
    const nr = b0 + b1 * z1r + b2 * z2r;
    const ni = b1 * z1i + b2 * z2i;
    const dr = 1 + a1 * z1r + a2 * z2r;
    const di = a1 * z1i + a2 * z2i;
    mag *= Math.hypot(nr, ni) / Math.hypot(dr, di);
  }
  return 20 * Math.log10(Math.max(mag, 1e-9));
}

/* ══════════════════════════════════════════════════════════════
   canvas
   ══════════════════════════════════════════════════════════════ */
const canvas = $('curve');
const ctx = canvas.getContext('2d');
let W = 960, H = 400, DPR = 1;
const M = { l: 52, r: 18, t: 18, b: 38 };

const X = (f) => M.l + (Math.log10(f) - Math.log10(FMIN)) /
  (Math.log10(FMAX) - Math.log10(FMIN)) * (W - M.l - M.r);
const Y = (g) => M.t + (GMAX - g) / (GMAX - GMIN) * (H - M.t - M.b);
const freqAt = (x) => Math.pow(10, Math.log10(FMIN) +
  (x - M.l) / (W - M.l - M.r) * (Math.log10(FMAX) - Math.log10(FMIN)));
const gainAt = (y) => GMAX - (y - M.t) / (H - M.t - M.b) * (GMAX - GMIN);

function resize() {
  const rect = canvas.getBoundingClientRect();
  DPR = window.devicePixelRatio || 1;
  W = Math.max(rect.width, 320);
  H = Math.max(rect.height, 240);
  canvas.width = Math.round(W * DPR);
  canvas.height = Math.round(H * DPR);
  ctx.setTransform(DPR, 0, 0, DPR, 0, 0);
  draw();
}

function drawGrid() {
  ctx.lineWidth = 1;
  ctx.font = '11px -apple-system, "PingFang SC", system-ui, sans-serif';

  // frequency grid
  const freqs = [20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000];
  for (const f of freqs) {
    const x = Math.round(X(f)) + 0.5;
    ctx.strokeStyle = f === 1000 ? C.gridStrong : C.grid;
    ctx.beginPath(); ctx.moveTo(x, M.t); ctx.lineTo(x, H - M.b); ctx.stroke();
    ctx.fillStyle = C.axis;
    ctx.textAlign = 'center';
    const label = f >= 1000 ? (f / 1000) + 'k' : String(f);
    ctx.fillText(label, x, H - M.b + 17);
  }

  // gain grid
  for (let g = GMIN; g <= GMAX; g += 5) {
    const y = Math.round(Y(g)) + 0.5;
    ctx.strokeStyle = g === 0 ? C.gridStrong : C.grid;
    ctx.beginPath(); ctx.moveTo(M.l, y); ctx.lineTo(W - M.r, y); ctx.stroke();
    ctx.fillStyle = C.axis;
    ctx.textAlign = 'right';
    ctx.fillText((g > 0 ? '+' : '') + g, M.l - 9, y + 4);
  }

}

function curvePath(bands) {
  const p = new Path2D();
  const steps = 420;
  const lo = Math.log10(FMIN), hi = Math.log10(FMAX);
  for (let i = 0; i <= steps; i++) {
    const f = Math.pow(10, lo + (hi - lo) * i / steps);
    const y = Y(Math.max(GMIN - 3, Math.min(GMAX + 3, responseAt(bands, f, state.fs))));
    const x = X(f);
    if (i === 0) p.moveTo(x, y); else p.lineTo(x, y);
  }
  return p;
}

function drawCurve(bands, opts) {
  const path = curvePath(bands);
  if (opts.fill) {
    const g = ctx.createLinearGradient(0, M.t, 0, H - M.b);
    g.addColorStop(0, 'rgba(126,216,190,.22)');
    g.addColorStop(1, 'rgba(126,216,190,.01)');
    ctx.save();
    ctx.lineTo(X(FMAX), Y(0));
    ctx.lineTo(X(FMIN), Y(0));
    ctx.closePath();
    ctx.fillStyle = g;
    ctx.fill(path);
    ctx.restore();
  }
  ctx.save();
  ctx.lineWidth = opts.width || 2.4;
  ctx.strokeStyle = opts.color || C.curve;
  ctx.shadowColor = opts.glow || 'rgba(126,216,190,.35)';
  ctx.shadowBlur = opts.glow ? 12 : 0;
  ctx.stroke(path);
  ctx.restore();
}

function drawNodes() {
  const bands = state.draft && state.draft.bands;
  if (!bands) return;
  bands.forEach((b, i) => {
    const x = X(b.freq), y = Y(b.gain);
    const active = i === state.drag || i === state.hover;
    const r = active ? 9 : 7;

    ctx.beginPath();
    ctx.arc(x, y, r + 4, 0, Math.PI * 2);
    ctx.fillStyle = BAND_COLORS[i] + '22';
    ctx.fill();

    ctx.beginPath();
    ctx.arc(x, y, r, 0, Math.PI * 2);
    ctx.fillStyle = BAND_COLORS[i];
    ctx.fill();
    ctx.lineWidth = 2;
    ctx.strokeStyle = C.canvas;
    ctx.stroke();

    ctx.fillStyle = C.canvas;
    ctx.font = 'bold 10px "Roboto", system-ui, sans-serif';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(String(i + 1), x, y + 0.5);
    ctx.textBaseline = 'alphabetic';

    if (active) {
      const txt = `${b.freq >= 1000 ? (b.freq / 1000).toFixed(2) + 'k' : b.freq} Hz · ${b.gain > 0 ? '+' : ''}${b.gain.toFixed(1)} dB · Q ${b.q.toFixed(2)}`;
      ctx.font = '11.5px -apple-system, "PingFang SC", system-ui, sans-serif';
      const w = ctx.measureText(txt).width + 16;
      const bx = Math.min(Math.max(x - w / 2, M.l + 4), W - M.r - w - 4);
      const by = Math.min(Math.max(y - 38, M.t + 4), H - M.b - 30);
      ctx.fillStyle = 'rgba(47,54,52,.97)';
      ctx.strokeStyle = '#3F4945';
      ctx.lineWidth = 1;
      roundRect(bx, by, w, 22, 8);
      ctx.fill(); ctx.stroke();
      ctx.fillStyle = '#DDE4E0';
      ctx.textAlign = 'left';
      ctx.fillText(txt, bx + 8, by + 15);
    }
  });
}

function roundRect(x, y, w, h, r) {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

function draw() {
  if (!W || !H) return;
  ctx.clearRect(0, 0, W, H);
  drawGrid();

  if (state.unsaved && state.live && state.live.bands) {
    drawCurve(state.live.bands, { color: C.curveSoft, width: 1.5 });
  }
  if (state.draft && state.draft.bands) {
    drawCurve(state.draft.bands, { fill: true, color: C.curve });
  }
  drawNodes();
}

/* ══════════════════════════════════════════════════════════════
   device sync
   ══════════════════════════════════════════════════════════════ */
let applyTimer = null;

function scheduleApply() {
  clearTimeout(applyTimer);
  applyTimer = setTimeout(applyNow, 160);
}

async function applyNow() {
  if (!state.draft || state.applying) return;
  state.applying = true;
  try {
    const eq = await api(`/api/eq/${state.bank}`, {
      method: 'PUT',
      body: JSON.stringify({ enabled: state.enabled, bands: state.draft.bands }),
    });
    state.live = eq;
    setUnsaved(true);
  } catch (e) {
    toast(e.message, 'err');
    if (e.status === 503) setConnected(false);
  } finally {
    state.applying = false;
  }
}

function setUnsaved(v) {
  state.unsaved = v;
  $('unsaved').classList.toggle('hidden', !v);
}

async function loadBank(bank) {
  try {
    const eq = await api(`/api/eq/${bank}`);
    state.live = eq;
    state.draft = { enabled: eq.enabled, bands: eq.bands.map((b) => ({ ...b })) };
    state.enabled = eq.enabled;
    $('eqEnabled').checked = eq.enabled;
    setUnsaved(false);
    renderBands();
    draw();
  } catch (e) {
    toast(`读取 ${bank.toUpperCase()} 失败：${e.message}`, 'err');
    if (e.status === 503) setConnected(false);
  }
}

function setConnected(on, info) {
  state.connected = on;
  const box = $('statusBox');
  box.classList.toggle('on', on);
  box.classList.toggle('off', !on);
  $('statusText').textContent = on
    ? (info ? `${info.product} · FW ${info.version}` : '已连接')
    : '耳机未连接';
  $('btnSave').disabled = !on;
  $('btnRead').disabled = !on;
  $('btnFactory').disabled = !on;

  if (info) {
    state.info = info;
    $('deviceLine').textContent =
      `${info.product} · FW ${info.version} · ${info.vidpid} · 序列号 ${info.serial}`;
  }
}

/* ══════════════════════════════════════════════════════════════
   band rows
   ══════════════════════════════════════════════════════════════ */
function renderBands() {
  const host = $('bands');
  host.innerHTML = '';
  if (!state.draft) return;

  state.draft.bands.forEach((b, i) => {
    const row = document.createElement('div');
    row.className = 'band';
    row.innerHTML = `
      <span class="band-idx" style="--c:${BAND_COLORS[i]}">${i + 1}</span>
      <label class="tf">
        <span class="tf-label">类型</span>
        <select data-f="type">
          ${Object.keys(TYPE_LABEL).map((t) =>
            `<option value="${t}" ${t === b.type ? 'selected' : ''}>${TYPE_LABEL[t]}</option>`).join('')}
        </select>
      </label>
      <label class="tf">
        <span class="tf-label">Hz</span>
        <input data-f="freq" type="number" min="20" max="20000" step="1" value="${b.freq}">
      </label>
      <label class="tf">
        <span class="tf-label">dB</span>
        <input data-f="gain" type="number" min="-15" max="15" step="0.1" value="${b.gain.toFixed(1)}">
      </label>
      <label class="tf">
        <span class="tf-label">Q</span>
        <input data-f="q" type="number" min="0.1" max="10" step="0.01" value="${b.q.toFixed(2)}">
      </label>
    `;
    row.querySelectorAll('[data-f]').forEach((el) => {
      el.addEventListener('change', () => {
        const f = el.dataset.f;
        let v = el.value;
        if (f !== 'type') {
          v = parseFloat(v);
          if (!isFinite(v)) { renderBands(); return; }
          if (f === 'freq') v = Math.min(20000, Math.max(20, Math.round(v)));
          if (f === 'gain') v = Math.min(15, Math.max(-15, Math.round(v * 10) / 10));
          if (f === 'q') v = Math.min(10, Math.max(0.1, Math.round(v * 100) / 100));
        }
        state.draft.bands[i][f] = v;
        draw();
        scheduleApply();
      });
    });
    host.appendChild(row);
  });
}

function syncRows(i) {
  const row = $('bands').children[i];
  if (!row) return;
  const b = state.draft.bands[i];
  row.querySelector('[data-f=freq]').value = b.freq;
  row.querySelector('[data-f=gain]').value = b.gain.toFixed(1);
  row.querySelector('[data-f=q]').value = b.q.toFixed(2);
}

/* ══════════════════════════════════════════════════════════════
   canvas interaction
   ══════════════════════════════════════════════════════════════ */
function pickNode(ev) {
  if (!state.draft) return -1;
  const rect = canvas.getBoundingClientRect();
  const mx = ev.clientX - rect.left, my = ev.clientY - rect.top;
  let best = -1, bestD = 18;
  state.draft.bands.forEach((b, i) => {
    const d = Math.hypot(X(b.freq) - mx, Y(b.gain) - my);
    if (d < bestD) { bestD = d; best = i; }
  });
  return best;
}

canvas.addEventListener('pointerdown', (ev) => {
  const i = pickNode(ev);
  if (i < 0) return;
  state.drag = i;
  canvas.setPointerCapture(ev.pointerId);
  canvas.style.cursor = 'grabbing';
  draw();
});

canvas.addEventListener('pointermove', (ev) => {
  const rect = canvas.getBoundingClientRect();
  const mx = ev.clientX - rect.left;

  if (state.drag >= 0) {
    const my = ev.clientY - rect.top;
    const f = Math.min(FMAX, Math.max(FMIN, Math.round(freqAt(mx))));
    const g = Math.min(GMAX, Math.max(GMIN, Math.round(gainAt(my) * 10) / 10));
    state.draft.bands[state.drag].freq = f;
    state.draft.bands[state.drag].gain = g;
    syncRows(state.drag);
    draw();
    return;
  }

  const h = pickNode(ev);
  if (h !== state.hover) {
    state.hover = h;
    canvas.style.cursor = h >= 0 ? 'grab' : 'crosshair';
    draw();
  }
  if (mx >= M.l && mx <= W - M.r) {
    const f = freqAt(mx);
    $('cursorFreq').textContent = f >= 1000 ? (f / 1000).toFixed(2) + ' kHz' : Math.round(f) + ' Hz';
  } else {
    $('cursorFreq').textContent = '';
  }
});

function endDrag() {
  if (state.drag < 0) return;
  state.drag = -1;
  canvas.style.cursor = 'grab';
  draw();
  scheduleApply();
}
canvas.addEventListener('pointerup', endDrag);
canvas.addEventListener('pointercancel', endDrag);

canvas.addEventListener('dblclick', (ev) => {
  const i = pickNode(ev);
  if (i < 0) return;
  state.draft.bands[i].gain = 0;
  syncRows(i);
  draw();
  scheduleApply();
});

canvas.addEventListener('wheel', (ev) => {
  const i = pickNode(ev);
  if (i < 0) return;
  ev.preventDefault();
  const b = state.draft.bands[i];
  const factor = Math.exp(-ev.deltaY * 0.0012);
  b.q = Math.min(10, Math.max(0.1, Math.round(b.q * factor * 100) / 100));
  syncRows(i);
  draw();
  scheduleApply();
}, { passive: false });

/* ══════════════════════════════════════════════════════════════
   controls
   ══════════════════════════════════════════════════════════════ */
document.querySelectorAll('.seg-btn[data-bank]').forEach((t) => {
  t.addEventListener('click', async () => {
    if (t.dataset.bank === state.bank) return;
    document.querySelectorAll('.seg-btn[data-bank]').forEach((x) => {
      x.classList.toggle('active', x === t);
      x.setAttribute('aria-selected', x === t ? 'true' : 'false');
    });
    state.bank = t.dataset.bank;
    state.draft = null;
    renderBands();
    await loadBank(state.bank);
  });
});

$('eqEnabled').addEventListener('change', (e) => {
  state.enabled = e.target.checked;
  scheduleApply();
});

$('preamp').addEventListener('input', (e) => {
  state.preamp = parseFloat(e.target.value);
  updateFill(e.target);
  $('preampVal').textContent = (state.preamp > 0 ? '+' : '') + state.preamp.toFixed(1) + ' dB';
});

$('preamp').addEventListener('change', async () => {
  try {
    const p = await api('/api/preamp', {
      method: 'PUT',
      body: JSON.stringify({ l: state.preamp, r: state.preamp }),
    });
    state.preamp = p.l;
    state.savedPreamp = p.l;
    setUnsaved(true);
  } catch (e) {
    toast(e.message, 'err');
  }
});

$('btnRead').addEventListener('click', async () => {
  await refreshAll();
  await loadBank(state.bank);
  toast('已从耳机读取', 'ok');
});

$('btnSave').addEventListener('click', async () => {
  const btn = $('btnSave');
  btn.disabled = true;
  btn.textContent = '写入中…';
  try {
    await applyNow();
    await api('/api/save', { method: 'POST' });
    setUnsaved(false);
    toast('已写入耳机 flash —— 换台电脑插上还是这套调音', 'ok');
    // device rebooted; give it a moment, then resync
    await sleep(1200);
    await refreshAll();
    await loadBank(state.bank);
  } catch (e) {
    toast(`写入失败：${e.message}`, 'err');
  } finally {
    btn.disabled = false;
    btn.textContent = '存到耳机';
  }
});

$('btnFactory').addEventListener('click', async () => {
  if (!confirm('恢复天使吉米出厂调音并写入 flash？')) return;
  const btn = $('btnFactory');
  btn.disabled = true;
  try {
    await api('/api/factory', { method: 'POST' });
    toast('已恢复出厂调音', 'ok');
    await sleep(1200);
    await refreshAll();
    await loadBank(state.bank);
    $('preamp').value = 0;
    $('preampVal').textContent = '0.0 dB';
    setUnsaved(false);
  } catch (e) {
    toast(`恢复失败：${e.message}`, 'err');
  } finally {
    btn.disabled = false;
  }
});

/* ══════════════════════════════════════════════════════════════
   presets
   ══════════════════════════════════════════════════════════════ */
function renderPresets() {
  const host = $('presets');
  host.innerHTML = '';
  state.presets.forEach((p) => {
    const b = document.createElement('button');
    b.className = 'chip';
    b.textContent = p.name;
    b.title = p.desc;
    b.addEventListener('click', () => {
      state.draft.bands = p.bands.map((x) => ({ ...x }));
      document.querySelectorAll('.chip').forEach((c) => c.classList.toggle('active', c === b));
      renderBands();
      draw();
      scheduleApply();
      toast(`已套用预设：${p.name}`);
    });
    host.appendChild(b);
  });
}

/* ══════════════════════════════════════════════════════════════
   AutoEq / profile import-export
   ══════════════════════════════════════════════════════════════ */
const TYPE_MAP = {
  PK: 'PK', LS: 'LSH', LSC: 'LSH', HS: 'HSH', HSC: 'HSH',
  LP: 'LPF', HP: 'HPF', LPQ: 'LPF', HPQ: 'HPF',
};

function parseAutoEq(text) {
  const out = [];
  let preamp = null;
  const pm = text.match(/Preamp:\s*(-?[\d.]+)\s*dB/i);
  if (pm) preamp = parseFloat(pm[1]);
  const re = /Filter\s+\d+:\s+(ON|OFF)\s+([A-Za-z]+)\s+Fc\s+([\d.]+)\s*Hz\s+Gain\s+(-?[\d.]+)\s*dB\s+Q\s+([\d.]+)/gi;
  let m;
  while ((m = re.exec(text)) !== null) {
    out.push({
      freq: Math.round(parseFloat(m[3])),
      gain: Math.round(parseFloat(m[4]) * 10) / 10,
      q: Math.round(parseFloat(m[5]) * 100) / 100,
      type: TYPE_MAP[m[2].toUpperCase()] || 'PK',
    });
  }
  return { bands: out, preamp };
}

async function setPreamp(db) {
  db = Math.max(-12, Math.min(6, Math.round(db * 2) / 2));
  state.preamp = db;
  $('preamp').value = db;
  updateFill($('preamp'));
  $('preampVal').textContent = (db > 0 ? '+' : '') + db.toFixed(1) + ' dB';
  try {
    const p = await api('/api/preamp', { method: 'PUT', body: JSON.stringify({ l: db, r: db }) });
    state.savedPreamp = p.l;
    setUnsaved(true);
  } catch (e) {
    toast(e.message, 'err');
  }
}

$('btnImport').addEventListener('click', () => {
  const text = $('autoeq').value.trim();
  if (!text) { toast('先粘贴 AutoEq 文本或 JSON', 'err'); return; }
  let bands, preamp = null;
  try {
    if (text.startsWith('{')) {
      const j = JSON.parse(text);
      bands = (j.bands || []).map((b) => ({ ...b }));
      preamp = typeof j.preamp === 'number' ? j.preamp : null;
    } else {
      ({ bands, preamp } = parseAutoEq(text));
    }
  } catch (e) {
    toast('解析失败：' + e.message, 'err');
    return;
  }
  if (!bands || !bands.length) { toast('没解析到滤波器行', 'err'); return; }
  const truncated = bands.length > 5;
  bands = bands.slice(0, 5);
  while (bands.length < 5) bands.push({ freq: 1000, gain: 0, q: 0.7, type: 'PK' });
  state.draft.bands = bands;
  renderBands();
  draw();
  scheduleApply();
  if (preamp !== null && isFinite(preamp)) setPreamp(preamp);
  toast(`已导入 ${Math.min(5, bands.length)} 段` + (truncated ? '（超过 5 段，已截断）' : ''), 'ok');
});

$('btnExport').addEventListener('click', () => {
  const data = {
    device: state.info ? state.info.product : 'KT DSP',
    bank: state.bank,
    enabled: state.enabled,
    preamp: state.preamp,
    bands: state.draft.bands,
  };
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `bunny-${state.bank}-${new Date().toISOString().slice(0, 10)}.json`;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  toast('已导出当前曲线', 'ok');
});

/* ══════════════════════════════════════════════════════════════
   polling
   ══════════════════════════════════════════════════════════════ */
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function refreshAll() {
  try {
    const st = await api('/api/status');
    if (st.version) $('versionTag').textContent = 'bunnyctl ' + st.version;
    setConnected(st.connected, st.info);
    if (st.connected && st.preamp) {
      state.savedPreamp = st.preamp.l;
      if (document.activeElement !== $('preamp')) {
        state.preamp = st.preamp.l;
        $('preamp').value = st.preamp.l;
        updateFill($('preamp'));
        $('preampVal').textContent = (st.preamp.l > 0 ? '+' : '') + st.preamp.l.toFixed(1) + ' dB';
      }
    }
    return st.connected;
  } catch (e) {
    setConnected(false);
    return false;
  }
}

async function boot() {
  resize();
  attachRipple();
  updateFill($('preamp'));
  try {
    state.presets = await api('/api/presets');
    renderPresets();
  } catch { /* ignore */ }

  const ok = await refreshAll();
  if (ok) await loadBank(state.bank);
  else toast('没找到耳机，插好 USB 后点「从耳机读取」', 'err');

  setInterval(async () => {
    const wasConnected = state.connected;
    const st = await api('/api/status').catch(() => null);
    if (!st) return;
    setConnected(st.connected, st.info);
    if (st.connected && !wasConnected && !state.draft) await loadBank(state.bank);
  }, 3000);
}

window.addEventListener('resize', resize);
boot();
