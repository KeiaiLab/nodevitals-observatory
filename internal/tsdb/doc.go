// Package tsdb 는 nodevitals-observatory 의 자체 시계열 저장 엔진이다.
//
// 계층 구조 (아래에서 위로):
//
//	bstream       비트 단위 스트림
//	encoding_*    타임스탬프 delta-of-delta · 값 XOR
//	chunk         두 인코더를 묶은 append-only 청크
//	head          인메모리 시리즈 + 열린 청크
//	wal           head 를 크래시로부터 보호
//	block         head 를 굳힌 불변 디렉터리
//	querier       head + block 통합 조회
//	rollup        5분 집계 블록
//	retention     보존기간 초과 블록 삭제
//	db            위 전부를 조립한 공개 API
//
// 설계 근거는 nodevitals repo 의
// docs/superpowers/specs/2026-07-24-nodevitals-observatory-design.md §4 참조.
package tsdb

// Version 은 저장 포맷 버전이다. 블록 meta.json 에 기록되며, 포맷이
// 호환 불가하게 바뀔 때만 올린다.
const Version = "1"
