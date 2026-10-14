package masknpy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"testing"
)

func arrayFixture(shape string, values []float32) []byte {
	header := fmt.Sprintf("{'descr':'<f4','fortran_order':False,'shape':%s}\n", shape)
	body := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(body[8:], uint16(len(header)))
	body = append(body, header...)
	for _, v := range values {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
		body = append(body, b[:]...)
	}
	return body
}
func TestArraysExactDimensionsFiniteAndUnitPolicy(t *testing.T) {
	for _, channels := range []int{0, 2, 3} {
		n := channels
		if n == 0 {
			n = 1
		}
		shape := "(2,3)"
		if channels != 0 {
			shape = fmt.Sprintf("(2,3,%d)", channels)
		}
		values := make([]float32, 6*n)
		for i := range values {
			values[i] = float32(i) - 2
		}
		verify := func(body []byte, unit bool) error {
			sum := sha256.Sum256(body)
			return VerifyFloat32ArrayNPY(context.Background(), bytes.NewReader(body), int64(len(body)), hex.EncodeToString(sum[:]), 3, 2, channels, unit)
		}
		if err := verify(arrayFixture(shape, values), false); err != nil {
			t.Fatal(channels, err)
		}
		if err := verify(arrayFixture(shape, values), true); err == nil {
			t.Fatal("out-of-unit values accepted")
		}
		for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
			values[0] = bad
			if err := verify(arrayFixture(shape, values), false); err == nil {
				t.Fatal("nonfinite accepted")
			}
		}
		values[0] = 0
		for _, badShape := range []string{"(3,2)", "(2,3,1)", "(2,3,4)", "(2,3,2,1)", "(2,3,__import__('os'))"} {
			if err := verify(arrayFixture(badShape, values), false); err == nil {
				t.Fatal("bad shape accepted", channels, badShape)
			}
		}
	}
}

func TestAlphaUint16AndFloat32Policies(t *testing.T) {
	header := "{'descr':'<u2','fortran_order':False,'shape':(2,3)}\n"
	body := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(body[8:], uint16(len(header)))
	body = append(body, header...)
	for _, v := range []uint16{0, 1, 32768, 65535, 1234, 65534} {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], v)
		body = append(body, b[:]...)
	}
	verify := func(raw []byte) error {
		sum := sha256.Sum256(raw)
		return VerifyAlphaNPY(context.Background(), bytes.NewReader(raw), int64(len(raw)), hex.EncodeToString(sum[:]), 3, 2)
	}
	if err := verify(body); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{body[:len(body)-1], append(append([]byte(nil), body...), 0), bytes.Replace(body, []byte("<u2"), []byte(">u2"), 1), bytes.Replace(body, []byte("<u2"), []byte("<i2"), 1)} {
		if err := verify(raw); err == nil {
			t.Fatal("invalid alpha accepted")
		}
	}
	if err := verify(arrayFixture("(2,3)", []float32{0, .5, 1, 0, 1, .25})); err != nil {
		t.Fatal(err)
	}
	if err := verify(arrayFixture("(2,3)", []float32{0, .5, 1, 0, 1, 1.1})); err == nil {
		t.Fatal("alpha >1 accepted")
	}
	sum := sha256.Sum256(body)
	if err := VerifyFloat32MaskNPY(context.Background(), bytes.NewReader(body), int64(len(body)), hex.EncodeToString(sum[:]), 3, 2); err == nil {
		t.Fatal("uint16 accepted as mask")
	}
}

func TestUint16RGBExactShapeAndType(t *testing.T) {
	header := "{'descr':'<u2','fortran_order':False,'shape':(2,3,3)}\n"
	body := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(body[8:], uint16(len(header)))
	body = append(body, header...)
	for i := 0; i < 18; i++ {
		var sample [2]byte
		binary.LittleEndian.PutUint16(sample[:], uint16(i*3600))
		body = append(body, sample[:]...)
	}
	verify := func(raw []byte) error {
		sum := sha256.Sum256(raw)
		return VerifyUint16RGBNPY(context.Background(), bytes.NewReader(raw),
			int64(len(raw)), hex.EncodeToString(sum[:]), 3, 2)
	}
	if err := verify(body); err != nil { t.Fatal(err) }
	for _, invalid := range [][]byte{
		body[:len(body)-1],
		append(append([]byte(nil), body...), 0),
		bytes.Replace(body, []byte("<u2"), []byte(">u2"), 1),
		bytes.Replace(body, []byte("(2,3,3)"), []byte("(3,2,3)"), 1),
		arrayFixture("(2,3,3)", make([]float32, 18)),
	} {
		if err := verify(invalid); err == nil { t.Fatal("invalid uint16 RGB accepted") }
	}
}
