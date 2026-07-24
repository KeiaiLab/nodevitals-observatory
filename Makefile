IMAGE ?= ghcr.io/keiailab/nodevitals-observatory:dev

.PHONY: all fmt vet test build clean web-build docker

all: fmt vet test

fmt:
	gofmt -l -w .

vet:
	go vet ./...

test:
	go test ./... -race

bench:
	go test ./internal/tsdb/ -bench=. -benchmem -run=^$$

# web-build: 로컬 개발용 콘솔 빌드(M5) — go:embed 대상인 internal/webui/assets
# 를 채운다. Dockerfile 은 자체 web 스테이지에서 --frozen-lockfile 로 별도 수행
# 하므로(재현성), 이 타깃은 docker 타깃과 독립적이다(중복 빌드 없음).
web-build:
	cd web && pnpm install && pnpm build

# docker: multi-stage 이미지 빌드(web → go → distroless). 거버넌스 §2.3 —
# linux/amd64 단일 아키텍처(멀티아키 금지) 고정.
docker:
	docker build --platform=linux/amd64 -t $(IMAGE) .

clean:
	rm -rf dist/
