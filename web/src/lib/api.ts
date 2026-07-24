// api.ts — observatory API 클라이언트 (m5-design.md §4.1).
// 모든 호출은 credentials:'include' 로 세션 쿠키를 동봉하고, 보호 엔드포인트의
// 401 은 등록된 핸들러(AuthContext)로 통지해 로그인 화면 전환을 일으킨다.
// 번들에 시크릿/토큰 0 — 인증 상태는 전적으로 HttpOnly 쿠키가 담는다.

export class UnauthorizedError extends Error {
  constructor(path: string) {
    super(`observatory: 401 unauthorized — ${path}`);
    this.name = 'UnauthorizedError';
  }
}

type UnauthorizedHandler = () => void;

let onUnauthorized: UnauthorizedHandler | null = null;

/** AuthContext 가 마운트 시 등록 — 보호 API 401 수신 시 anon 전환 콜백. */
export function setUnauthorizedHandler(handler: UnauthorizedHandler | null): void {
  onUnauthorized = handler;
}

// notify=false 는 로그인/로그아웃 자체의 401(자격 오류)이 세션만료 통지와
// 섞이지 않게 한다 — 실패 표시는 호출자(Login 폼)가 담당.
async function request(path: string, init: RequestInit, notify: boolean): Promise<Response> {
  const res = await fetch(path, { credentials: 'include', ...init });
  if (res.status === 401) {
    if (notify) onUnauthorized?.();
    throw new UnauthorizedError(path);
  }
  if (!res.ok) {
    throw new Error(`observatory: HTTP ${res.status} — ${path}`);
  }
  return res;
}

async function apiFetch<T>(path: string): Promise<T> {
  const res = await request(path, {}, true);
  return (await res.json()) as T;
}

// ---- 응답 스키마 (Prometheus HTTP API 호환 봉투) ----

export interface ApiEnvelope<T> {
  status: 'success';
  data: T;
}

export interface InstantSample {
  metric: Record<string, string>;
  value: [number, string];
}

export interface VectorData {
  resultType: 'vector';
  result: InstantSample[];
}

export interface RangeSeries {
  metric: Record<string, string>;
  values: Array<[number, string]>;
}

export interface MatrixData {
  resultType: 'matrix';
  result: RangeSeries[];
}

export type SeriesData = Array<Record<string, string>>;

export type LabelsData = string[];

// ---- API 표면 ----

export const api = {
  /** POST /api/v1/auth/login — 성공 204(세션 쿠키 발급), 실패 UnauthorizedError. */
  async login(username: string, password: string): Promise<void> {
    await request(
      '/api/v1/auth/login',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      },
      false,
    );
  },

  /** POST /api/v1/auth/logout — 항상 204 (본문·Content-Type 없음). */
  async logout(): Promise<void> {
    await request('/api/v1/auth/logout', { method: 'POST' }, false);
  },

  /** GET /api/v1/query — instant vector. */
  instantQuery(selector: string): Promise<ApiEnvelope<VectorData>> {
    const params = new URLSearchParams({ query: selector });
    return apiFetch(`/api/v1/query?${params.toString()}`);
  },

  /** GET /api/v1/query_range — matrix (M3-lite, 초 단위 start/end/step). */
  rangeQuery(
    selector: string,
    startSec: number,
    endSec: number,
    stepSec: number,
  ): Promise<ApiEnvelope<MatrixData>> {
    const params = new URLSearchParams({
      query: selector,
      start: String(startSec),
      end: String(endSec),
      step: String(stepSec),
    });
    return apiFetch(`/api/v1/query_range?${params.toString()}`);
  },

  /** GET /api/v1/series — match[] 반복 파라미터. */
  series(matches: string[]): Promise<ApiEnvelope<SeriesData>> {
    const params = new URLSearchParams();
    for (const m of matches) {
      params.append('match[]', m);
    }
    return apiFetch(`/api/v1/series?${params.toString()}`);
  },

  /** GET /api/v1/labels — 최경량 보호 엔드포인트(인증 프로브 겸용). */
  labels(): Promise<ApiEnvelope<LabelsData>> {
    return apiFetch('/api/v1/labels');
  },
};
