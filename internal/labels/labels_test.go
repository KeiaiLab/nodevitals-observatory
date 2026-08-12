package labels

import "testing"

func TestLabels_항상_이름순_정렬된다(t *testing.T) {
	ls := NewLabels(
		Label{"node", "e101"},
		Label{"__name__", "node_load1"},
		Label{"tier", "core"},
	)
	want := []string{"__name__", "node", "tier"}
	for i, w := range want {
		if ls[i].Name != w {
			t.Fatalf("위치 %d: got %q, want %q", i, ls[i].Name, w)
		}
	}
}

func TestLabels_해시는_입력_순서와_무관하다(t *testing.T) {
	a := NewLabels(Label{"node", "e101"}, Label{"tier", "core"})
	b := NewLabels(Label{"tier", "core"}, Label{"node", "e101"})
	if a.Hash() != b.Hash() {
		t.Fatalf("같은 라벨셋의 해시가 다르다: %d vs %d", a.Hash(), b.Hash())
	}
}

func TestLabels_다른_라벨셋은_다른_해시를_낸다(t *testing.T) {
	a := NewLabels(Label{"node", "e101"})
	b := NewLabels(Label{"node", "e102"})
	if a.Hash() == b.Hash() {
		t.Fatal("서로 다른 라벨셋이 같은 해시를 냈다")
	}
	// 구분자 없이 이어붙이면 아래 두 개가 충돌한다 — 구분자가 있는지 확인.
	c := NewLabels(Label{"a", "bc"})
	d := NewLabels(Label{"ab", "c"})
	if c.Hash() == d.Hash() {
		t.Fatal("구분자 부재로 해시가 충돌한다")
	}
}

func TestLabels_Get과_Equal(t *testing.T) {
	ls := NewLabels(Label{"node", "e101"}, Label{"device", "sda"})
	if got := ls.Get("node"); got != "e101" {
		t.Fatalf("Get(node): got %q", got)
	}
	if got := ls.Get("없음"); got != "" {
		t.Fatalf("없는 라벨은 빈 문자열이어야 한다, got %q", got)
	}
	if !ls.Equal(NewLabels(Label{"device", "sda"}, Label{"node", "e101"})) {
		t.Fatal("같은 라벨셋인데 Equal 이 false")
	}
	if ls.Equal(NewLabels(Label{"node", "e101"})) {
		t.Fatal("길이가 다른데 Equal 이 true")
	}
}

func TestLabels_String은_PromQL_표기를_낸다(t *testing.T) {
	ls := NewLabels(Label{"__name__", "node_load1"}, Label{"node", "e101"})
	want := `{__name__="node_load1", node="e101"}`
	if got := ls.String(); got != want {
		t.Fatalf("String: got %q, want %q", got, want)
	}
}

func TestLabels_Copy는_원본과_분리된다(t *testing.T) {
	orig := NewLabels(Label{"node", "e101"})
	cp := orig.Copy()
	cp[0].Value = "e999"
	if orig[0].Value != "e101" {
		t.Fatal("Copy 가 원본을 공유한다")
	}
}

func TestLabelsFromMap(t *testing.T) {
	ls := LabelsFromMap(map[string]string{"tier": "core", "node": "e101"})
	if len(ls) != 2 || ls[0].Name != "node" || ls[1].Name != "tier" {
		t.Fatalf("정렬된 2개여야 한다: %v", ls)
	}
}

func TestLabels_빈_라벨셋(t *testing.T) {
	empty := NewLabels()
	if len(empty) != 0 {
		t.Fatal("빈 NewLabels 는 길이 0 이어야 한다")
	}
	if empty.Hash() == 0 {
		t.Fatal("빈 라벨셋의 해시도 일정해야 한다 (0 일 필요는 없음)")
	}
	hash1 := empty.Hash()
	hash2 := empty.Hash()
	if hash1 != hash2 {
		t.Fatal("같은 빈 라벨셋의 해시가 다르다")
	}
	if got := empty.String(); got != "{}" {
		t.Fatalf("빈 라벨셋 String: got %q, want %q", got, "{}")
	}
}

func TestLabels_NewLabels가_입력_슬라이스를_복사한다(t *testing.T) {
	input := []Label{{Name: "node", Value: "e101"}}
	ls := NewLabels(input...)
	// 원본 수정
	input[0].Value = "e999"
	// 라벨셋은 영향받지 않아야 함
	if ls[0].Value != "e101" {
		t.Fatal("NewLabels 가 입력 슬라이스를 공유한다")
	}
}

func TestLabels_중복_이름은_필터링되지_않는다(t *testing.T) {
	// 같은 이름으로 여러 라벨을 전달
	ls := NewLabels(
		Label{"node", "e101"},
		Label{"node", "e102"},
	)
	// 이 계층은 중복 이름을 걸러내지 않는다 — 정렬 후 둘 다 남고 Get 은
	// 앞쪽 것을 반환한다. 중복을 막는 것은 상위 계층(수집기)의 책임이다.
	if len(ls) != 2 {
		t.Fatalf("중복 이름도 그대로 유지: len=%d", len(ls))
	}
	// 정렬되었으므로 순서 불변
	if ls[0].Name != "node" || ls[1].Name != "node" {
		t.Fatal("정렬이 이상하다")
	}
}

func TestLabels_Empty_Hash_Stability(t *testing.T) {
	// 빈 라벨셋과 같은 라벨셋의 해시 일정성
	empty1 := LabelsFromMap(map[string]string{})
	empty2 := NewLabels()
	if empty1.Hash() != empty2.Hash() {
		t.Fatal("빈 라벨셋의 해시가 다르다")
	}
}

func TestLabels_Single_Label(t *testing.T) {
	ls := NewLabels(Label{"node", "e101"})
	if len(ls) != 1 || ls[0].Name != "node" || ls[0].Value != "e101" {
		t.Fatal("단일 라벨이 제대로 유지되지 않음")
	}
	if got := ls.String(); got != `{node="e101"}` {
		t.Fatalf("String: got %q", got)
	}
}

func TestLabels_MapKey는_충돌하지_않는다(t *testing.T) {
	// String() 은 이름을 이스케이프하지 않아 이 두 라벨셋이 같은 문자열을 낸다.
	a := NewLabels(Label{"a", "b"}, Label{"c", "d"})
	b := NewLabels(Label{`a="b", c`, "d"})

	if a.String() != b.String() {
		t.Fatalf("전제가 깨졌다 — String() 이 이미 구분한다: %q vs %q", a.String(), b.String())
	}
	if a.MapKey() == b.MapKey() {
		t.Fatalf("MapKey 가 충돌한다: %q", a.MapKey())
	}
}

func TestLabels_MapKey는_같은_라벨셋에_같은_키를_준다(t *testing.T) {
	a := NewLabels(Label{"node", "e101"}, Label{"tier", "core"})
	b := NewLabels(Label{"tier", "core"}, Label{"node", "e101"}) // 입력 순서만 다름

	if a.MapKey() != b.MapKey() {
		t.Fatalf("같은 라벨셋의 MapKey 가 다르다: %q vs %q", a.MapKey(), b.MapKey())
	}
	if (Labels{}).MapKey() != "" {
		t.Fatalf("빈 라벨셋의 MapKey 는 빈 문자열이어야 한다")
	}
}
