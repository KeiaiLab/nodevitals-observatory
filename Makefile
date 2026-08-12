.PHONY: all fmt vet test build clean web-build

# 외부 의존성 0 이 이 제품의 전제다 — go.mod 에 require 가 늘면 "차트 하나 =
# 파드 하나 = 완결" 이 성립하지 않는다. dep-check 가 그것을 지킨다.
all: dep-check fmt vet test

.PHONY: dep-check
dep-check:
	@if grep -qE '^\s*require' go.mod; then \
		echo "FAIL: go.mod 에 require 가 생겼다 — 이 제품은 표준 라이브러리만 쓴다"; \
		grep -nE '^\s*require' go.mod; exit 1; \
	fi
	@echo "PASS: 외부 의존성 0"

fmt:
	@out=$$(gofmt -l ./cmd ./internal 2>/dev/null); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./... -coverprofile cover.out

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o dist/observatory ./cmd/observatory

clean:
	rm -rf dist cover.out

# 프론트를 빌드한다. vite 의 outDir 이 internal/webui/assets 라 산출물이 바로
# embed 대상에 떨어진다 — 복사 단계를 두면 emptyOutDir 와 순서가 엉켜 방금
# 만든 번들을 지운다(실제로 그렇게 한 번 깨뜨렸다).
#
# 번들은 커밋하지 않고 index.html 셸만 커밋한다. 빌드 전에는 셸이 참조하는
# 해시 번들이 없으므로 Go 테스트가 그 상태를 인식해 관련 검사를 건너뛴다 —
# 그래야 프론트를 빌드하지 않은 개발자의 `make all` 이 실패하지 않는다.
web-build:
	cd web && pnpm install --frozen-lockfile && pnpm build
