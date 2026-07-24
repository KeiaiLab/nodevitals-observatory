# syntax=docker/dockerfile:1
# observatory 는 순수 Go(외부 의존 0, cgo 0)라 정적 바이너리 + distroless/static.
# (nodevitals 는 go-nvml cgo 라 cc-debian12 였음 — 여기선 해당 없음.)
# --platform=$BUILDPLATFORM: 빌더 스테이지를 호스트 아키텍처로 네이티브 실행하고
# Go 는 GOARCH 로 크로스컴파일한다 — arm64 맥에서 amd64 이미지를 QEMU 에뮬레이션
# 없이 빠르게 만든다(순수 Go, cgo 0 이라 가능). web 스테이지도 순수 JS 빌드라
# 크로스컴파일 불필요 — 같은 이유로 네이티브 실행.
#
# ── stage 1: web — pnpm 빌드 → internal/webui/assets (M5, go:embed 대상) ──
FROM --platform=$BUILDPLATFORM node:26-bookworm-slim AS web
WORKDIR /src/web
# node:26 은 corepack 을 동봉하지 않으므로(corepack: not found, exit 127) web/package.json
# 의 packageManager 핀(pnpm@11.15.1)과 동일 버전을 npm 으로 전역 설치해 고정한다.
RUN npm install -g pnpm@11.15.1
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ── stage 2: builder — go 빌드(assets 임베드 + 정적 바이너리) ──
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod ./
# 외부 의존 0 계약 — go mod download 는 no-op 이지만 캐시 레이어 유지 목적으로 둔다.
RUN go mod download
COPY . .
# 커밋된 placeholder(internal/webui/assets/index.html)를 web 스테이지의 실빌드
# 산출물로 대체 — go:embed 는 이 COPY 이후, go build 이전에 이미 채워져 있다.
COPY --from=web /src/internal/webui/assets internal/webui/assets
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/observatory ./cmd/observatory

# ── stage 3: 기존 distroless/static 유지 ──
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source=https://github.com/KeiaiLab/nodevitals-observatory
COPY --from=builder /out/observatory /observatory
USER nonroot
ENTRYPOINT ["/observatory"]
