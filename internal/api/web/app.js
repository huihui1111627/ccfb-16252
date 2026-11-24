const $ = (id) => document.getElementById(id);
let overview = null;
let selectedFlightId = null;
let detailData = null;
let detailTimer = null;
let detailFilter = '';
let expandedBags = new Set();

function esc(v) {
  if (v === null || v === undefined) return '';
  return String(v).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function fmtMin(t) {
  if (t === null || t === undefined || Number.isNaN(t)) return '--:--';
  const v = ((Math.round(t) % 1440) + 1440) % 1440;
  return `${String(Math.floor(v / 60)).padStart(2, '0')}:${String(v % 60).padStart(2, '0')}`;
}
function pct(v, digits = 1) { return `${Number(v || 0).toFixed(digits)}%`; }

const flightStatusMap = {
  scheduled: ['计划', 'gray'], boarding: ['登机中', 'blue'], final_call: ['最后呼叫', 'amber'],
  reconcile_blocked: ['核对拦截', 'red'], closing: ['关舱中', 'violet'], departed: ['已起飞', 'green'],
  delayed: ['延误', 'amber'], cancelled: ['取消', 'red']
};
const bagStatusMap = {
  planned: ['已计划', 'gray'], bsm_published: ['报文已发', 'blue'], await_induct: ['待上线', 'gray'],
  inducted: ['分拣线上', 'blue'], scanning: ['扫描中', 'blue'], scanned: ['已扫描', 'blue'],
  missing_message: ['无报文', 'red'], chute_assigned: ['已分出口', 'green'], at_chute: ['出口待装', 'green'],
  exception_chute: ['异常出口', 'red'], recall_chute: ['召回中', 'amber'], loaded_container: ['已装集装箱', 'violet'],
  misloaded: ['错装', 'red'], offloaded: ['已卸下', 'amber'], recovered: ['找回重支', 'blue'],
  departed: ['已离港', 'green'], voided: ['撤销作废', 'gray'], delivered: ['已交付', 'green'],
  transfer_hold: ['中转待提', 'violet'], failed: ['失败', 'red']
};
const scenarioStatusMap = { scheduled: ['待触发', 'gray'], running: ['执行中', 'blue'], passed: ['已验证', 'green'], failed: ['未通过', 'red'] };
function badge(map, key, text) {
  const [label, cls] = map[key] || [key || '未知', 'gray'];
  return `<span class="badge ${cls}">${esc(text || label)}</span>`;
}

async function fetchJSON(url, opts) {
  const res = await fetch(url, opts);
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`);
  return res.json();
}
async function control(action, speed) {
  await fetchJSON('/api/control', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action, speed })
  });
  loadOverview();
}

function renderKpis(s) {
  const items = [
    { label: '托运行李报文', value: s.totalBags.toLocaleString(), sub: `${s.totalPassengers.toLocaleString()} 名旅客 · 确定性枢纽一天`, cls: 'blue' },
    { label: '行李最终去向正确', value: pct(s.correctnessRate, 2), sub: `${s.correctBags.toLocaleString()} 正确 / ${s.incorrectBags} 错误 / ${s.pendingAuditBags} 在途`, cls: s.incorrectBags ? 'bad' : 'good' },
    { label: '分拣线在流', value: s.activeSorterBags, sub: `${s.exceptionBags} 件在异常/无报文出口`, cls: s.exceptionBags ? 'warn' : 'blue' },
    { label: '未登机行李定位', value: pct(s.locationAccuracyRate, 0), sub: `${s.locationAccurateCases}/${s.noShowCases} 件定位准确 · 平均 ${s.avgRetrievalSeconds.toFixed(0)} 秒`, cls: 'good' },
    { label: '中转最终衔接', value: pct(s.finalConnectRate, 1), sub: `原航班 ${s.transferMadeOriginal.toLocaleString()} · 保护改签 ${s.transferAutoRebooked}`, cls: s.transferFailed ? 'warn' : 'good' },
    { label: '关舱演练缺陷', value: `${s.detectedProblemFlights}/${s.plantedProblemFlights}`, sub: `漏检 ${s.missedProblemFlights} · 拦截放行 ${s.blockedReleases} 次`, cls: s.missedProblemFlights ? 'bad' : 'good' },
    { label: '航班放行/起飞', value: `${s.departedFlights}`, sub: `${s.reconciledFlights} 班完成核对 · ${s.releasedFlights} 班放行`, cls: 'blue' },
    { label: '中间件报文', value: s.messagesPublished.toLocaleString(), sub: `${s.orphanScans} 次无报文扫描 · ${s.orphanResolved} 次核实恢复`, cls: '' }
  ];
  $('kpiGrid').innerHTML = items.map(k => `
    <div class="kpi ${k.cls}">
      <div class="label">${k.label}</div>
      <div class="value">${k.value}</div>
      <div class="sub">${k.sub}</div>
    </div>`).join('');
}

function renderAssurance(s) {
  const rows = [
    ['最终去向正确率', s.correctnessRate],
    ['卸载定位准确率', s.locationAccuracyRate],
    ['中转最终成功率', s.finalConnectRate],
    ['问题航班发现率', s.plantedProblemFlights ? s.detectedProblemFlights / s.plantedProblemFlights * 100 : 100]
  ];
  $('assurance').innerHTML = `
    <div class="assurance-grid">
      ${rows.map(([name, val]) => `
        <div class="metric-line">
          <span>${name}</span>
          <span class="track"><i style="width:${Math.max(0, Math.min(100, val))}%"></i></span>
          <strong>${pct(val, val === 100 ? 0 : 1)}</strong>
        </div>`).join('')}
    </div>
    <p class="assurance-note">
      旧流程盲找平均约 <b>40 分钟</b>；新中间件直接给出集装箱、层和货位，本次平均实物取出约
      <b>${s.avgRetrievalSeconds.toFixed(0)} 秒</b>，查询定位为毫秒级。<br>
      关舱规则：已登机旅客的行李必须在机上；未登机/取消旅客的行李必须不在机上；不一致即禁止放行。
    </p>`;
}

function renderSorter(o) {
  $('bagDots').innerHTML = o.sorterBags.map(b => `
    <div class="bag-dot ${b.color}" style="left:calc(3% + ${(b.progress || .2) * 94}%)" title="${esc(b.tag)} · ${esc(b.state)} · ${esc(b.chuteId || '')}">
    </div>`).join('');

  $('chuteGrid').innerHTML = o.chutes.map(c => {
    const cls = c.kind === 'exception' ? 'exception' : c.kind === 'recall' ? 'recall' : (c.status === 'busy' ? 'busy' : '');
    const title = c.kind === 'exception' ? c.name : c.kind === 'recall' ? c.name : `${c.id} 分拣出口`;
    return `<div class="chute ${cls}">
      <h3><span>${esc(c.id)}</span><span class="muted">${o.chuteActiveCounts[c.id] || c.activeBagCount || 0}件</span></h3>
      ${c.flightNumber ? `<div class="flight">${esc(c.flightNumber)}</div><div class="dest">→ ${esc(c.destination)}</div><div class="meta"><span>口 ${esc(c.gate || '-')}</span><span>${esc(c.status === 'busy' ? '繁忙' : '在用')}</span></div>`
        : `<div class="dest" style="margin-top:9px">${esc(title)}</div><div class="meta"><span>${c.kind === 'flight' ? '空闲/预排' : '专用异常路由'}</span><span>累计 ${c.bagCount || 0}</span></div>`}
    </div>`;
  }).join('');
}

function recBadge(f) {
  const recs = f.reconciliations || [];
  if (!recs.length) return '<span class="badge gray">未核对</span>';
  const r = recs[recs.length - 1];
  if (r.passed) return `<span class="badge green">通过${recs.length > 1 ? '·复核' + recs.length : ''}</span>`;
  return `<span class="badge red">拦截 ${r.issues ? r.issues.length : 0}</span>`;
}

function renderFlights(o) {
  const q = ($('flightSearch').value || '').trim().toLowerCase();
  let flights = o.flights.filter(f => f.kind === 'outbound');
  if (q) flights = flights.filter(f => [f.number, f.destination, f.origin, f.gate, f.chuteId, f.aircraftReg].join(' ').toLowerCase().includes(q));
  flights = flights.slice(0, 220);
  $('flightTbody').innerHTML = flights.map(f => {
    const delay = f.estimatedAt > f.scheduledAt + 4 ? `<div class="badge amber">预计 ${fmtMin(f.estimatedAt)}</div>` : '';
    const tags = (f.scenarioTags || []).slice(0, 2).map(t => `<span class="badge violet">演练</span>`).join('');
    return `<tr class="flight-row" data-id="${esc(f.id)}">
      <td><div class="flight-no">${esc(f.number)}</div><div class="muted">${esc(f.id)} ${tags}</div></td>
      <td><div class="route">${esc(f.origin)} → ${esc(f.destination)}</div><div class="muted">${f.international ? '国际/地区' : '国内'}</div></td>
      <td><div>${fmtMin(f.scheduledAt)}</div>${delay}</td>
      <td><div>${esc(f.gate)} / <b>${esc(f.chuteId)}</b></div><div class="muted">${esc(f.aircraftModel)}</div></td>
      <td>${badge(flightStatusMap, f.status)}</td>
      <td>${recBadge(f)}</td>
      <td>${(o.flightBagCounts[f.id] || 0).toLocaleString()}</td>
    </tr>`;
  }).join('');
  document.querySelectorAll('.flight-row').forEach(tr => tr.addEventListener('click', () => openFlight(tr.dataset.id)));
}

function renderScenarios(o) {
  $('scenarioList').innerHTML = (o.scenarios || []).map(sc => `
    <div class="scenario">
      <div class="top"><h4>${esc(sc.title)}</h4>${badge(scenarioStatusMap, sc.status)}</div>
      <p>${esc(sc.summary || '等待在模拟时钟 ' + fmtMin(sc.scheduledAt) + ' 触发')}</p>
    </div>`).join('');
}

function renderAlerts(o) {
  $('alertList').innerHTML = (o.alerts || []).map(a => `
    <div class="alert ${esc(a.level)}">
      <div class="meta"><span>${fmtMin(a.at)} · ${esc(a.topic)}</span><span>${a.resolved ? '已闭环' : ''}</span></div>
      <strong>${esc(a.title)}</strong>
      <p>${esc(a.message)}</p>
    </div>`).join('');
}

function payloadSummary(p) {
  if (!p) return '';
  const keys = ['tag', 'flightId', 'oldFlightId', 'newFlightId', 'chuteId', 'newChute', 'container', 'layer', 'passed', 'issues', 'count', 'delayMinutes'];
  return keys.filter(k => p[k] !== undefined).map(k => `${k}=${typeof p[k] === 'object' ? JSON.stringify(p[k]) : p[k]}`).join(' · ');
}
function renderMessages(o) {
  $('messageList').innerHTML = (o.events || []).map(m => `
    <div class="message">
      <time>${fmtMin(m.at)}</time>
      <span class="type">${esc(m.type)}</span>
      <span><span class="key">${esc(m.key || '')}</span><div class="muted">${esc(payloadSummary(m.payload))}</div></span>
    </div>`).join('');
}

async function loadOverview() {
  try {
    const o = await fetchJSON('/api/overview');
    overview = o;
    $('clock').textContent = o.clock;
    $('dayProgress').style.width = pct(o.dayProgress * 100, 2);
    $('runState').innerHTML = o.running ? '<span class="badge green">模拟运行中</span>' : '<span class="badge gray">已暂停/日终</span>';
    $('speedLabel').textContent = `${o.speed} 模拟分/秒`;
    $('playBtn').textContent = o.running ? '暂停' : '继续';
    document.querySelectorAll('.speed').forEach(b => b.classList.toggle('active', Number(b.dataset.speed) === Number(o.speed)));
    renderKpis(o.stats); renderAssurance(o.stats); renderSorter(o); renderFlights(o); renderScenarios(o); renderAlerts(o); renderMessages(o);
  } catch (e) {
    $('runState').textContent = '接口连接失败';
    console.error(e);
  }
}

function bagLocation(b) {
  if (!b.containerCode) return '<span class="muted">未进集装箱</span>';
  return `<b>${esc(b.containerCode)}</b><div class="muted">${esc(b.layerLabel || ('第' + b.layer + '层'))} · ${esc(b.slot || '')}</div>`;
}

function renderDetail(d) {
  detailData = d;
  const f = d.flight;
  $('detailTitle').textContent = `${f.number} · ${f.origin} → ${f.destination}`;
  $('detailSub').textContent = `${f.id} · 计划 ${fmtMin(f.scheduledAt)} · 登机口 ${f.gate} · 出口 ${f.chuteId} · ${f.aircraftModel} ${f.aircraftReg}`;
  const rec = d.reconciliation;
  const recHtml = rec ? `
    <div class="reconcile ${rec.passed ? 'pass' : 'fail'}">
      <h3>${rec.passed ? '✅ 关舱核对通过，可放行' : '⛔ 关舱核对未通过，禁止放行'}</h3>
      <div class="muted">${fmtMin(rec.at)} · 已登机旅客 ${rec.boardedPax} · 应随机行李 ${rec.boardedBags} · 实物在机 ${rec.onboardBags}</div>
      ${(rec.issues || []).map(i => `<div class="issue"><b>${esc(i.type)}</b> · 标签 <span class="tag-link">${esc(i.bagTag)}</span> · ${esc(i.passengerName)}<br>${esc(i.detail)}<br><span class="muted">期望：${esc(i.expected)}；实际：${esc(i.actual)}；处置：${esc(i.remediation)}</span></div>`).join('')}
      ${rec.note ? `<p style="margin-bottom:0">${esc(rec.note)}</p>` : ''}
    </div>` : '<div class="reconcile pass"><h3>尚未到关舱核对时刻</h3><span class="muted">将在计划起飞前 10 分钟自动执行三方核对。</span></div>';

  const gateHistory = (f.gateHistory || []).map(x => `<span class="badge amber">${fmtMin(x.at)} ${x.oldGate}→${x.newGate}</span>`).join(' ');
  const airHistory = (f.aircraftHistory || []).map(x => `<span class="badge amber">${fmtMin(x.at)} ${x.oldReg}→${x.newReg}</span>`).join(' ');

  const q = detailFilter.trim().toLowerCase();
  let bags = d.bags || [];
  if (q) bags = bags.filter(b => [b.tag, b.shortTag, b.passengerName, b.pnr, b.containerCode, b.status, b.layerLabel].join(' ').toLowerCase().includes(q));

  const containers = d.containers || [];
  const containerHtml = containers.map(c => {
    const cb = (d.bags || []).filter(b => b.containerID === c.id);
    return `<div class="container-card">
      <h4><span>${esc(c.code)}</span><span class="muted">${c.bagCount}/${c.capacity}</span></h4>
      <div class="layer-bar">
        <div class="layer">顶层<b>${c.layerCounts ? c.layerCounts[2] : 0}</b></div>
        <div class="layer">中层<b>${c.layerCounts ? c.layerCounts[1] : 0}</b></div>
        <div class="layer">底层<b>${c.layerCounts ? c.layerCounts[0] : 0}</b></div>
      </div>
      ${cb.slice(0, 18).map(b => `<div class="mini-bag"><span>${esc(b.shortTag)} · ${esc(b.passengerName || '')}</span><span>${b.layer || '-'}层</span></div>`).join('')}
      ${cb.length > 18 ? `<div class="muted" style="font-size:10px;margin-top:5px">另有 ${cb.length - 18} 件见装载清单</div>` : ''}
    </div>`;
  }).join('');

  $('detailBody').innerHTML = `
    <div class="detail-grid">
      <div class="detail-card"><div class="k">计划/预计起飞</div><div class="v">${fmtMin(f.scheduledAt)} / ${fmtMin(f.estimatedAt)}</div></div>
      <div class="detail-card"><div class="k">登机/核对/起飞</div><div class="v">${fmtMin(f.boardingAt)} / ${fmtMin(f.reconcileAt)} / ${fmtMin(f.departAt)}</div></div>
      <div class="detail-card"><div class="k">当前状态</div><div class="v">${badge(flightStatusMap, f.status)}</div></div>
      <div class="detail-card"><div class="k">放行状态</div><div class="v">${f.released ? '<span class="badge green">已放行</span>' : f.releaseBlocked ? '<span class="badge red">禁止放行</span>' : '<span class="badge gray">待核对</span>'}</div></div>
    </div>
    <div style="margin-bottom:12px;display:flex;gap:8px;flex-wrap:wrap">${gateHistory}${airHistory}</div>
    ${recHtml}
    <div class="panel" style="box-shadow:none;background:rgba(255,255,255,.02)">
      <div class="panel-head"><div><h2>集装箱 / 货舱积载</h2><p>扫描装车时记录集装箱、层和具体货位；未登机旅客可直接按此定位。</p></div></div>
      <div class="container-grid" style="padding:0 18px 18px">${containerHtml}</div>
    </div>
    <div class="manifest-tools">
      <input id="bagFilter" class="input" style="min-width:320px" placeholder="在本航班 ${d.bags.length} 件行李中搜索标签/旅客/集装箱" value="${esc(detailFilter)}" />
      <span class="muted">筛选出 ${bags.length} 件；点击行展开完整报文轨迹</span>
    </div>
    <div class="table-wrap manifest-table">
      <table>
        <thead><tr><th>标签号</th><th>旅客/PNR</th><th>状态</th><th>当前/原始航班</th><th>集装箱与层位</th><th>货位</th><th>定位/轨迹</th></tr></thead>
        <tbody>
          ${bags.slice(0, 500).map(b => {
            const open = expandedBags.has(b.tag);
            const retrieval = b.retrieval ? `<div class="muted">定位：${esc(b.retrieval.predictedCode)} ${b.retrieval.predictedLayer}层 · 取出 ${b.retrieval.completedAt ? b.retrieval.elapsedSeconds.toFixed(0) + '秒' : '进行中'}</div>` : '';
            return `
            <tr class="bag-row" data-tag="${esc(b.tag)}">
              <td><span class="tag-link">${esc(b.tag)}</span>${b.transfer ? '<br><span class="badge violet">中转</span>' : ''}</td>
              <td>${esc(b.passengerName || '未关联/待核实')}<div class="muted">${esc(b.pnr || '')}</div></td>
              <td>${badge(bagStatusMap, b.status)}</td>
              <td>${esc(b.flightId)}${b.originalFlightId && b.originalFlightId !== b.flightId ? `<div class="muted">原 ${esc(b.originalFlightId)}</div>` : ''}</td>
              <td>${bagLocation(b)}</td>
              <td>${esc(b.slot || '-')}</td>
              <td>${retrieval}<button class="btn ghost" style="padding:4px 8px;font-size:11px" data-toggle="${esc(b.tag)}">${open ? '收起轨迹' : '轨迹'}</button></td>
            </tr>
            ${open ? `<tr class="history-row"><td colspan="7"><ul class="history-list">${(b.history || []).slice().reverse().map(e => `<li><b>${fmtMin(e.at)}</b> [${esc(e.type)}] ${esc(e.message)}${e.location ? ' — <span class="muted">' + esc(e.location) + '</span>' : ''}</li>`).join('')}</ul></td></tr>` : ''}`;
          }).join('')}
        </tbody>
      </table>
      ${bags.length > 500 ? '<p class="muted" style="padding:10px">结果较多，仅展示前 500 件；请用标签或集装箱筛选。</p>' : ''}
    </div>`;

  const filter = $('bagFilter');
  if (filter) {
    filter.addEventListener('input', () => { detailFilter = filter.value; renderDetail(detailData); });
    if (document.activeElement && document.activeElement.id === 'bagFilter') {
      filter.focus(); filter.setSelectionRange(filter.value.length, filter.value.length);
    }
  }
  document.querySelectorAll('[data-toggle]').forEach(btn => btn.addEventListener('click', (e) => {
    e.stopPropagation();
    const tag = btn.dataset.toggle;
    if (expandedBags.has(tag)) expandedBags.delete(tag); else expandedBags.add(tag);
    if (detailData) renderDetail(detailData);
  }));
}

async function refreshDetail() {
  if (!selectedFlightId || $('flightDrawer').hidden) return;
  try {
    const d = await fetchJSON(`/api/flights/${encodeURIComponent(selectedFlightId)}`);
    renderDetail(d);
  } catch (e) { console.error(e); }
}
async function openFlight(id) {
  selectedFlightId = id;
  if (!location.hash.includes(id)) history.replaceState(null, '', `#flight=${id}`);
  detailFilter = '';
  expandedBags = new Set();
  $('flightDrawer').hidden = false;
  $('drawerMask').hidden = false;
  $('detailBody').innerHTML = '<div class="loading">正在读取装载清单和集装箱定位…</div>';
  await refreshDetail();
  clearInterval(detailTimer);
  detailTimer = setInterval(refreshDetail, 1800);
}
function closeDrawer() {
  selectedFlightId = null;
  if (location.hash) history.replaceState(null, '', location.pathname + location.search);
  $('flightDrawer').hidden = true;
  $('drawerMask').hidden = true;
  clearInterval(detailTimer);
}

$('playBtn').addEventListener('click', () => control('toggle'));
$('finishBtn').addEventListener('click', () => control('finish'));
$('resetBtn').addEventListener('click', () => { if (confirm('重新生成相同种子的一天？当前模拟状态将清空。')) control('reset'); });
document.querySelectorAll('.speed').forEach(b => b.addEventListener('click', () => control('speed', Number(b.dataset.speed))));
$('flightSearch').addEventListener('input', () => { if (overview) renderFlights(overview); });
$('closeDrawer').addEventListener('click', closeDrawer);
$('drawerMask').addEventListener('click', closeDrawer);
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeDrawer(); });

loadOverview();
setInterval(loadOverview, 1000);
const initialFlight = location.hash.match(/flight=([A-Za-z0-9_-]+)/);
if (initialFlight) openFlight(initialFlight[1]);
window.addEventListener('hashchange', () => {
  const m = location.hash.match(/flight=([A-Za-z0-9_-]+)/);
  if (m && m[1] !== selectedFlightId) openFlight(m[1]);
});
