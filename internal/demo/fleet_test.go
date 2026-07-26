package demo

import "testing"

func TestBuildFleet_결정론과_규모(t *testing.T) {
	specs, err := ParseFleetSpec(defaultFleetSpec)
	if err != nil {
		t.Fatalf("기본 플릿 스펙 파싱 실패: %v", err)
	}

	f1 := BuildFleet(specs, 42)
	f2 := BuildFleet(specs, 42)

	if len(f1.GPUs) != 7000 {
		t.Fatalf("기본 플릿 GPU 수 = %d, want 7000", len(f1.GPUs))
	}
	if len(f1.Nodes) == 0 || len(f1.Clusters) == 0 {
		t.Fatalf("노드/클러스터가 비었다: nodes=%d clusters=%d", len(f1.Nodes), len(f1.Clusters))
	}

	// 같은 seed → 동일 인벤토리(uuid·배치·대역) — 재기동 안정성의 전제.
	for i := range f1.GPUs {
		a, b := f1.GPUs[i], f2.GPUs[i]
		if a.UUID != b.UUID || a.Instance != b.Instance || a.band != b.band || a.Allocated != b.Allocated {
			t.Fatalf("결정론 위반 idx=%d: %+v != %+v", i, a, b)
		}
	}

	// 다른 seed → 다른 uuid (전부 같을 수 없다).
	f3 := BuildFleet(specs, 43)
	same := 0
	for i := range f1.GPUs {
		if f1.GPUs[i].UUID == f3.GPUs[i].UUID {
			same++
		}
	}
	if same == len(f1.GPUs) {
		t.Fatalf("seed 가 달라도 uuid 가 전부 동일하다 — 해시 파생 오류")
	}

	// CSP 별 GPU 수가 스펙과 일치한다.
	perCSP := map[string]int{}
	for _, g := range f1.GPUs {
		perCSP[g.CSP]++
	}
	for _, s := range specs {
		if perCSP[s.ID] != s.GPUs {
			t.Fatalf("CSP %s GPU 수 = %d, want %d", s.ID, perCSP[s.ID], s.GPUs)
		}
	}

	// 할당 비율은 90±4% 대역이어야 한다(93% 목표의 해시 분산 허용).
	alloc := 0
	for _, g := range f1.GPUs {
		if g.Allocated {
			alloc++
		}
	}
	pct := float64(alloc) / float64(len(f1.GPUs)) * 100
	if pct < 89 || pct > 97 {
		t.Fatalf("할당 비율 %.1f%% — 기대 대역(89~97%%) 밖", pct)
	}

	// 장기 미사용 관리 대상이 최소 몇 개는 존재해야 효율 화면 서사가 성립한다.
	idle := 0
	for _, g := range f1.GPUs {
		if g.IdleDays > 0 {
			idle++
		}
	}
	if idle == 0 {
		t.Fatalf("장기 미사용 GPU 가 0개 — 효율 화면 데이터 없음")
	}
}

func TestParseFleetSpec_오류(t *testing.T) {
	cases := []string{
		"",
		"nhn:NHN",     // 필드 부족
		"nhn:NHN:-3",  // 음수
		"nhn:NHN:abc", // 비정수
		":표시:10",      // 빈 id
		"nhn::10",     // 빈 표시명
		"a:A:1,b:B",   // 두 번째 항목 필드 부족
	}
	for _, c := range cases {
		if _, err := ParseFleetSpec(c); err == nil {
			t.Errorf("ParseFleetSpec(%q) 가 에러를 내지 않았다", c)
		}
	}

	specs, err := ParseFleetSpec("a:클라우드 A:16,b:클라우드 B:8")
	if err != nil {
		t.Fatalf("정상 스펙 파싱 실패: %v", err)
	}
	if specs[0].Adapter != "native" || specs[1].Adapter != "mock" {
		t.Fatalf("어댑터 배정 오류: %+v", specs)
	}
}
