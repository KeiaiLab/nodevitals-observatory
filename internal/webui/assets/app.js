'use strict';

// NodeVitals Observatory 관제 콘솔 클라이언트. 빌드 체인 없는 순정 JS —
// 로그인/로그아웃 + Overview 데이터 렌더를 전부 여기서 다룬다. 서버 유래
// 문자열(instance 라벨 등)은 스크레이프 대상에서 온 신뢰 불가 입력이라
// textContent/createElement 로만 DOM 에 반영한다 — innerHTML 은 절대 쓰지
// 않는다(m4-design.md §2 XSS 방침).

// METRIC_LOAD/METRIC_GPU_TEMP: m4-design.md §2 가 정한 기본 exposition
// 메트릭 이름. 환경별로 실제 이름이 다르면 이 두 상수만 바꿔 재배포한다.
const METRIC_LOAD = 'node_load1';
const METRIC_GPU_TEMP = 'nodevitals_gpu_temperature_celsius';

const POLL_INTERVAL_MS = 15000;
const EM_DASH = '—';
const DEGREE = '°C';

// AuthRequiredError: /api/v1/* 가 401 을 낸 경우만 구분해서 던진다 — 그 외
// 네트워크/서버 에러(로그만 남김)와 분기해 둘 다 로그인 뷰로 보낸다.
class AuthRequiredError extends Error {}

let pollTimer = null;

function byId(id) {
  return document.getElementById(id);
}

function setText(id, text) {
  byId(id).textContent = text;
}

// ---- API 호출 ----

async function apiQuery(query) {
  const url = '/api/v1/query?query=' + encodeURIComponent(query);
  const res = await fetch(url, { method: 'GET', credentials: 'same-origin' });
  if (res.status === 401) {
    throw new AuthRequiredError('인증이 필요하다');
  }
  if (!res.ok) {
    throw new Error('observatory: 쿼리 실패(' + query + '): HTTP ' + res.status);
  }
  const body = await res.json();
  const result = body && body.data && body.data.result;
  return Array.isArray(result) ? result : [];
}

function apiLogin(username, password) {
  return fetch('/api/v1/auth/login', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: username, password: password }),
  });
}

function apiLogout() {
  return fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'same-origin' });
}

// ---- 시리즈 helper ----
// apiserver 응답 계약: value = [evalSeconds, "문자열로 인코딩된 값"].

function seriesValue(series) {
  return series.value[1];
}

function seriesNumber(series) {
  return Number(seriesValue(series));
}

function seriesInstance(series) {
  return (series.metric && series.metric.instance) || '';
}

// ---- KPI 산식 ----

function countUpDown(upResults) {
  let up = 0;
  let down = 0;
  for (const s of upResults) {
    if (seriesValue(s) === '1') {
      up++;
    } else {
      down++;
    }
  }
  return { up: up, down: down };
}

function average(results) {
  if (results.length === 0) return null;
  let sum = 0;
  for (const s of results) sum += seriesNumber(s);
  return sum / results.length;
}

function maxOf(results) {
  if (results.length === 0) return null;
  let max = -Infinity;
  for (const s of results) {
    const v = seriesNumber(s);
    if (v > max) max = v;
  }
  return max;
}

function sumOf(results) {
  let sum = 0;
  for (const s of results) sum += seriesNumber(s);
  return sum;
}

// instance → 대표값 맵(동일 instance 에 시리즈가 여럿이면 최댓값 채택 —
// 예: GPU 여러 장인 노드의 온도는 핫스팟 관제 의미로 최댓값이 맞다).
function byInstanceMax(results) {
  const m = new Map();
  for (const s of results) {
    const inst = seriesInstance(s);
    if (!inst) continue;
    const v = seriesNumber(s);
    if (!m.has(inst) || v > m.get(inst)) m.set(inst, v);
  }
  return m;
}

// ---- 렌더 ----

function renderKpis(data) {
  const updown = countUpDown(data.up);
  setText('kpi-nodes', String(data.up.length));
  setText('kpi-updown', updown.up + ' UP / ' + updown.down + ' DOWN');

  const avgLoad = average(data.load);
  setText('kpi-load', avgLoad === null ? EM_DASH : avgLoad.toFixed(2));

  const maxGpu = maxOf(data.gpu);
  setText('kpi-gputemp', maxGpu === null ? EM_DASH : maxGpu.toFixed(1) + DEGREE);

  setText('kpi-series', Math.round(sumOf(data.series)).toLocaleString('ko-KR'));
}

function renderNodeTable(data) {
  const loadByInstance = byInstanceMax(data.load);
  const gpuByInstance = byInstanceMax(data.gpu);

  const rows = data.up
    .map((s) => {
      const instance = seriesInstance(s) || '(unknown)';
      return {
        instance: instance,
        up: seriesValue(s) === '1',
        load: loadByInstance.has(instance) ? loadByInstance.get(instance) : null,
        gpuTemp: gpuByInstance.has(instance) ? gpuByInstance.get(instance) : null,
      };
    })
    .sort((a, b) => a.instance.localeCompare(b.instance));

  const tbody = byId('node-table-body');
  tbody.textContent = '';

  if (rows.length === 0) {
    tbody.appendChild(buildEmptyRow('등록된 노드가 없다'));
    return;
  }

  for (const row of rows) {
    tbody.appendChild(buildNodeRow(row));
  }
}

function buildEmptyRow(message) {
  const tr = document.createElement('tr');
  tr.className = 'empty-row';
  const td = document.createElement('td');
  td.colSpan = 4;
  td.textContent = message;
  tr.appendChild(td);
  return tr;
}

function buildNodeRow(row) {
  const tr = document.createElement('tr');

  const tdInstance = document.createElement('td');
  tdInstance.textContent = row.instance;
  tr.appendChild(tdInstance);

  const tdStatus = document.createElement('td');
  const badge = document.createElement('span');
  badge.className = 'badge ' + (row.up ? 'badge-up' : 'badge-down');
  badge.textContent = row.up ? 'UP' : 'DOWN';
  tdStatus.appendChild(badge);
  tr.appendChild(tdStatus);

  const tdLoad = document.createElement('td');
  tdLoad.textContent = row.load === null ? EM_DASH : row.load.toFixed(2);
  tr.appendChild(tdLoad);

  const tdGpu = document.createElement('td');
  tdGpu.textContent = row.gpuTemp === null ? EM_DASH : row.gpuTemp.toFixed(1) + DEGREE;
  tr.appendChild(tdGpu);

  return tr;
}

function formatNow() {
  const d = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds()) + ' 갱신';
}

// ---- 뷰 토글 ----

function showLogin() {
  byId('view-overview').hidden = true;
  byId('view-login').hidden = false;
  byId('login-password').value = '';
  byId('login-username').focus();
}

function showOverview() {
  byId('view-login').hidden = true;
  byId('view-overview').hidden = false;
}

function showLoginError(message) {
  const el = byId('login-error');
  el.textContent = message;
  el.hidden = false;
}

function hideLoginError() {
  const el = byId('login-error');
  el.hidden = true;
  el.textContent = '';
}

// ---- 갱신 사이클 ----

async function refresh() {
  const results = await Promise.all([
    apiQuery('observatory_up'),
    apiQuery(METRIC_LOAD),
    apiQuery(METRIC_GPU_TEMP),
    apiQuery('observatory_scrape_samples'),
  ]);
  const data = { up: results[0], load: results[1], gpu: results[2], series: results[3] };

  renderKpis(data);
  renderNodeTable(data);
  setText('last-updated', formatNow());
}

function startPolling() {
  stopPolling();
  pollTimer = setInterval(async () => {
    try {
      await refresh();
    } catch (err) {
      stopPolling();
      if (!(err instanceof AuthRequiredError)) {
        console.error('observatory: 주기 갱신 실패', err);
      }
      showLogin();
    }
  }, POLL_INTERVAL_MS);
}

function stopPolling() {
  if (pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

// 부트스트랩 인증 판정(m4-design.md §2): 페이지 로드 직후 refresh() 를
// 시도한다 — 그 첫 호출이 곧 observatory_up 조회이므로 200 이면 Overview
// 데이터가 그대로 채워지고, 401 이면 AuthRequiredError 로 로그인 뷰로
// 떨어진다. 별도 세션 조회 엔드포인트는 없다(D1).
async function bootstrap() {
  try {
    await refresh();
    showOverview();
    startPolling();
  } catch (err) {
    if (!(err instanceof AuthRequiredError)) {
      console.error('observatory: 초기 조회 실패', err);
    }
    showLogin();
  }
}

// ---- 이벤트 배선 ----

function initLoginForm() {
  byId('login-form').addEventListener('submit', async (ev) => {
    ev.preventDefault();
    hideLoginError();

    const username = byId('login-username').value;
    const password = byId('login-password').value;

    let res;
    try {
      res = await apiLogin(username, password);
    } catch (err) {
      showLoginError('서버에 연결할 수 없다');
      return;
    }

    if (res.status === 204) {
      await bootstrap();
      return;
    }

    let message = '인증 실패';
    try {
      const body = await res.json();
      if (body && typeof body.error === 'string' && body.error) {
        message = body.error;
      }
    } catch (_err) {
      // 본문 파싱 실패 시 기본 메시지 유지
    }
    showLoginError(message);
  });
}

function initLogoutButton() {
  byId('logout-btn').addEventListener('click', async () => {
    stopPolling();
    try {
      await apiLogout();
    } catch (err) {
      console.error('observatory: 로그아웃 요청 실패', err);
    }
    showLogin();
  });
}

document.addEventListener('DOMContentLoaded', () => {
  initLoginForm();
  initLogoutButton();
  bootstrap();
});
