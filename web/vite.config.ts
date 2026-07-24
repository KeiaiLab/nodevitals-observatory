import path from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// m5-design.md §1.3 (D1): base 는 기본값 '/', assetsDir 기본값 'assets' —
// 해시 자산 URL 이 /assets/<name>-<hash>.js 로 나와 임베드 서버의 /assets/*
// 라우트 계약과 1:1 정합한다. 출력은 go:embed 대상 디렉터리로 직접 낸다.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, 'src') },
  },
  build: {
    outDir: '../internal/webui/assets',
    // outDir 이 프로젝트 루트 밖이라 기본은 비우지 않음 — 스테일 해시 파일
    // 누적(임베드 비대)을 막기 위해 명시. placeholder index.html 은 커밋본이
    // SSOT 이고 빌드 후 git 워크트리에서 복원한다(m5-design.md §2.2).
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:9210',
      '/healthz': 'http://localhost:9210',
    },
  },
});
