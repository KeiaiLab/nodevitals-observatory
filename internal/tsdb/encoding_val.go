package tsdb

import (
	"math"
	"math/bits"
)

// 값은 직전 값과의 XOR 로 인코딩한다. 하드웨어 메트릭은 변화가 느려 XOR 의
// 유효 비트가 가운데 몇 비트에 몰리므로, leading/trailing 0 개수를 적고
// 가운데만 저장한다. 값이 같으면 1비트로 끝난다.
//
// leadingUnset 은 "아직 윈도우가 정해지지 않음" 표식이다 — 0 은 유효한
// leading 값이라 sentinel 로 쓸 수 없다.
const leadingUnset uint8 = 0xff

type valEncoder struct {
	n        int
	v        float64
	leading  uint8
	trailing uint8
}

func (e *valEncoder) append(b *bstream, v float64) {
	if e.n == 0 {
		b.writeBits(math.Float64bits(v), 64)
		e.v = v
		e.leading = leadingUnset
		e.n++
		return
	}

	xor := math.Float64bits(v) ^ math.Float64bits(e.v)
	if xor == 0 {
		b.writeBit(false)
	} else {
		b.writeBit(true)

		lead := uint8(bits.LeadingZeros64(xor))
		trail := uint8(bits.TrailingZeros64(xor))
		// leading 은 5비트(0~31)로만 적으므로 32 이상은 잘라 쓴다.
		if lead >= 32 {
			lead = 31
		}

		if e.leading != leadingUnset && lead >= e.leading && trail >= e.trailing {
			// 직전 윈도우가 이번 XOR 를 덮는다 — 윈도우를 다시 적지 않는다.
			b.writeBit(false)
			b.writeBits(xor>>e.trailing, int(64-e.leading-e.trailing))
		} else {
			b.writeBit(true)
			b.writeBits(uint64(lead), 5)
			sigbits := 64 - lead - trail
			// sigbits 는 1~64 인데 6비트로는 0~63 만 담긴다. 64 는 0 으로
			// 적고 디코더가 되살린다 (0 은 xor==0 경로라 실제로 안 나온다).
			b.writeBits(uint64(sigbits), 6)
			b.writeBits(xor>>trail, int(sigbits))
			e.leading, e.trailing = lead, trail
		}
	}

	e.v = v
	e.n++
}

type valDecoder struct {
	n        int
	v        float64
	leading  uint8
	trailing uint8
}

func (d *valDecoder) next(r *breader) (float64, error) {
	if d.n == 0 {
		u, err := r.readBits(64)
		if err != nil {
			return 0, err
		}
		d.v = math.Float64frombits(u)
		d.n++
		return d.v, nil
	}

	changed, err := r.readBit()
	if err != nil {
		return 0, err
	}
	if changed {
		newWindow, err := r.readBit()
		if err != nil {
			return 0, err
		}
		if newWindow {
			lead, err := r.readBits(5)
			if err != nil {
				return 0, err
			}
			mbits, err := r.readBits(6)
			if err != nil {
				return 0, err
			}
			if mbits == 0 {
				mbits = 64
			}
			d.leading = uint8(lead)
			d.trailing = 64 - d.leading - uint8(mbits)
		}
		sigbits := int(64 - d.leading - d.trailing)
		u, err := r.readBits(sigbits)
		if err != nil {
			return 0, err
		}
		xor := u << d.trailing
		d.v = math.Float64frombits(math.Float64bits(d.v) ^ xor)
	}

	d.n++
	return d.v, nil
}
