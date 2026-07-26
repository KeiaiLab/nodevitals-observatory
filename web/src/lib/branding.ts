// branding — 데모 모드 제품 네이밍 SSOT. 데모 인스턴스(임원 시연)에서는 콘솔이
// "GpuManager" 로 브랜딩되고, 실서비스는 기존 명칭을 유지한다.
// (/api/v1/demo/status 는 비인증이라 로그인 화면에서도 판별 가능.)

export function productName(demoEnabled: boolean | null | undefined): string {
  return demoEnabled ? 'GpuManager' : 'NodeVitals Observatory';
}

export function productTagline(demoEnabled: boolean | null | undefined): string {
  return demoEnabled ? '멀티클라우드 GPU 플릿 매니저' : '관제 콘솔';
}
