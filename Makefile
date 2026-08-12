.PHONY: all fmt vet test build clean

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
