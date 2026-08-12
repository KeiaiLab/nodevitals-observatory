package tsdb

// 타임스탬프는 delta-of-delta 로 인코딩한다. 스크레이프는 간격이 거의 일정해
// 대부분의 dod 가 0 이 되고, 그 경우 샘플당 1비트만 든다.
//
// 첫 두 샘플은 압축 없이 64비트로 적는다 — 청크당 16바이트 고정 오버헤드는
// 무시할 수 있고, varint 를 비트 스트림에 섞는 것보다 구현이 단순하다.

// bitRange 는 x 가 nbits 크기 2의 보수 범위에 들어가는지 본다.
func bitRange(x int64, nbits uint8) bool {
	return -((1<<(nbits-1))-1) <= x && x <= 1<<(nbits-1)
}

type tsEncoder struct {
	n     int
	t     int64 // 직전 타임스탬프
	delta int64 // 직전 델타
}

func (e *tsEncoder) append(b *bstream, t int64) {
	switch {
	case e.n == 0:
		b.writeBits(uint64(t), 64)
	case e.n == 1:
		d := t - e.t
		b.writeBits(uint64(d), 64)
		e.delta = d
	default:
		d := t - e.t
		dod := d - e.delta
		switch {
		case dod == 0:
			b.writeBit(false) // '0'
		case bitRange(dod, 14):
			b.writeBits(0b10, 2)
			b.writeBits(uint64(dod), 14)
		case bitRange(dod, 17):
			b.writeBits(0b110, 3)
			b.writeBits(uint64(dod), 17)
		case bitRange(dod, 20):
			b.writeBits(0b1110, 4)
			b.writeBits(uint64(dod), 20)
		default:
			b.writeBits(0b1111, 4)
			b.writeBits(uint64(dod), 64)
		}
		e.delta = d
	}
	e.t = t
	e.n++
}

type tsDecoder struct {
	n     int
	t     int64
	delta int64
}

// signed 는 nbits 로 기록된 2의 보수 값을 int64 로 복원한다.
func signed(u uint64, nbits uint8) int64 {
	if u > 1<<(nbits-1) {
		return int64(u) - (1 << nbits)
	}
	return int64(u)
}

func (d *tsDecoder) next(r *breader) (int64, error) {
	switch {
	case d.n == 0:
		u, err := r.readBits(64)
		if err != nil {
			return 0, err
		}
		d.t = int64(u)
	case d.n == 1:
		u, err := r.readBits(64)
		if err != nil {
			return 0, err
		}
		d.delta = int64(u)
		d.t += d.delta
	default:
		// 선행 1비트 개수로 dod 폭을 판별한다 (최대 4비트).
		var lead int
		for lead < 4 {
			bit, err := r.readBit()
			if err != nil {
				return 0, err
			}
			if !bit {
				break
			}
			lead++
		}
		var nbits uint8
		switch lead {
		case 0:
			nbits = 0 // dod == 0
		case 1:
			nbits = 14
		case 2:
			nbits = 17
		case 3:
			nbits = 20
		default:
			nbits = 64
		}
		var dod int64
		if nbits > 0 {
			u, err := r.readBits(int(nbits))
			if err != nil {
				return 0, err
			}
			if nbits == 64 {
				dod = int64(u)
			} else {
				dod = signed(u, nbits)
			}
		}
		d.delta += dod
		d.t += d.delta
	}
	d.n++
	return d.t, nil
}
