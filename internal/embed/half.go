package embed

import (
	"encoding/binary"
	"math"
)

// Vectors are stored as IEEE 754 half precision: half the bytes of
// float32, and for unit vectors of a few hundred dimensions a cosine off by
// well under 1e-3, far below any ranking margin.

// AppendHalf appends v to b as little-endian float16 values.
func AppendHalf(b []byte, v []float32) []byte {
	for _, x := range v {
		b = binary.LittleEndian.AppendUint16(b, toHalf(x))
	}
	return b
}

// FromHalf decodes little-endian float16 values into dst (grown as
// needed) and returns it.
func FromHalf(dst []float32, b []byte) []float32 {
	n := len(b) / 2
	if cap(dst) < n {
		dst = make([]float32, n)
	}
	dst = dst[:n]
	for i := range n {
		dst[i] = halfTable[binary.LittleEndian.Uint16(b[2*i:])]
	}
	return dst
}

// halfTable maps every float16 bit pattern to its float32 value: decoding
// a library's vectors is a table lookup per value.
var halfTable = func() *[1 << 16]float32 {
	var t [1 << 16]float32
	for h := range t {
		t[h] = fromHalf(uint16(h))
	}
	return &t
}()

// toHalf rounds f to the nearest float16, ties to even; overflow goes to
// infinity, tiny values to subnormals or zero.
func toHalf(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16(bits>>16) & 0x8000
	exp := int(bits>>23&0xff) - 127 + 15
	mant := bits & 0x7fffff
	switch {
	case bits&0x7fffffff == 0:
		return sign
	case bits>>23&0xff == 0xff: // inf or NaN
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 0x1f:
		return sign | 0x7c00
	case exp <= 0:
		if exp < -10 {
			return sign
		}
		mant |= 0x800000 // the implicit bit
		shift := uint(14 - exp)
		half := uint16(mant >> shift)
		rem := mant & (1<<shift - 1)
		mid := uint32(1) << (shift - 1)
		if rem > mid || rem == mid && half&1 == 1 {
			half++
		}
		return sign | half
	}
	half := sign | uint16(exp)<<10 | uint16(mant>>13)
	rem := mant & 0x1fff
	if rem > 0x1000 || rem == 0x1000 && half&1 == 1 {
		half++ // a carry into the exponent is the right rounding
	}
	return half
}

// fromHalf widens a float16 to float32 exactly.
func fromHalf(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch exp {
	case 0:
		if mant == 0 {
			return math.Float32frombits(sign)
		}
		// Subnormal: normalize it.
		e := uint32(127 - 15 + 1)
		for mant&0x400 == 0 {
			mant <<= 1
			e--
		}
		return math.Float32frombits(sign | e<<23 | (mant&0x3ff)<<13)
	case 0x1f:
		return math.Float32frombits(sign | 0xff<<23 | mant<<13)
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
}
