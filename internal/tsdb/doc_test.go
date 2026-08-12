package tsdb

import "testing"

// 패키지가 컴파일되고 상수가 노출되는지 확인하는 스모크 테스트.
// 이후 모든 태스크가 이 패키지 위에 쌓인다.
func TestPackageVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version 이 비어 있다")
	}
}
