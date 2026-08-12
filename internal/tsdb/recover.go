package tsdb

import "github.com/KeiaiLab/nodevitals-observatory/internal/labels"

// RecoverHead 는 WAL 디렉터리를 재생해 head 를 복원한다.
//
// 손상·절단된 레코드는 ReplayWAL 이 조용히 잘라내므로, 여기서는 "재생된
// 만큼만 head 에 채운다"는 단순한 계약만 지킨다. 재생 중 만난 시리즈는
// 원래 ref 를 그대로 되살려야 이후 recSamples 레코드가 붙을 수 있다.
func RecoverHead(dir string) (*Head, error) {
	h := NewHead()

	err := ReplayWAL(dir,
		func(ref uint64, ls labels.Labels) error {
			h.GetOrCreateWithRef(ref, ls)
			return nil
		},
		func(ss []RefSample) error {
			for _, s := range ss {
				// 시리즈 레코드가 손상돼 없어졌을 수 있다 — 그런 샘플은
				// 버린다(라벨셋을 모르면 어차피 조회할 수 없다).
				if h.Series(s.Ref) == nil {
					continue
				}
				if err := h.AppendRef(s.Ref, s.T, s.V); err != nil {
					// 역행 샘플은 버리고 계속한다. WAL 은 append 순서를
					// 보존하므로 정상 경로에서는 발생하지 않는다.
					continue
				}
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return h, nil
}
