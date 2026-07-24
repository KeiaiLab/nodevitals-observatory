# syntax=docker/dockerfile:1
# observatory 는 순수 Go(외부 의존 0, cgo 0)라 정적 바이너리 + distroless/static.
# (nodevitals 는 go-nvml cgo 라 cc-debian12 였음 — 여기선 해당 없음.)
# --platform=$BUILDPLATFORM: 빌더 스테이지를 호스트 아키텍처로 네이티브 실행하고
# Go 는 GOARCH 로 크로스컴파일한다 — arm64 맥에서 amd64 이미지를 QEMU 에뮬레이션
# 없이 빠르게 만든다(순수 Go, cgo 0 이라 가능).
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod ./
# 외부 의존 0 계약 — go mod download 는 no-op 이지만 캐시 레이어 유지 목적으로 둔다.
RUN go mod download
COPY . .
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/observatory ./cmd/observatory

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source=https://github.com/KeiaiLab/nodevitals-observatory
COPY --from=builder /out/observatory /observatory
USER nonroot
ENTRYPOINT ["/observatory"]
