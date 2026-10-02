package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func maskNPY(header string, values []float32) []byte {
	body := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(body[8:], uint16(len(header)))
	body = append(body, []byte(header)...)
	for _, value := range values {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(value))
		body = append(body, b[:]...)
	}
	return body
}
func maskSHA(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }

func TestUserMaskNPYExactShapeProbabilityAndSHA(t *testing.T) {
	header := "{'descr': '<f4', 'fortran_order': False, 'shape': (2, 3), }\n"
	valid := maskNPY(header, []float32{0, .25, 1, .5, 0, 1})
	if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(valid), int64(len(valid)), maskSHA(valid), 3, 2); err != nil {
		t.Fatal(err)
	}
	for _, version := range []byte{2, 3} {
		var length [4]byte
		binary.LittleEndian.PutUint32(length[:], uint32(binary.LittleEndian.Uint16(valid[8:10])))
		body := append([]byte(nil), valid[:8]...)
		body[6] = version
		body = append(body, length[:]...)
		body = append(body, valid[10:]...)
		if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(body), int64(len(body)), maskSHA(body), 3, 2); err != nil {
			t.Fatal("NPY version", version, err)
		}
	}
	reordered := maskNPY("{\"shape\": (2,3), \"descr\": \"<f4\", \"fortran_order\": False}\n", []float32{0, .25, 1, .5, 0, 1})
	if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(reordered), int64(len(reordered)), maskSHA(reordered), 3, 2); err != nil {
		t.Fatal("valid header order", err)
	}
	large := maskNPY("{'shape':(1,20000),'descr':'<f4','fortran_order':False}\n", make([]float32, 20000))
	if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(large), int64(len(large)), maskSHA(large), 20000, 1); err != nil {
		t.Fatal("multi-buffer mask", err)
	}
	for _, values := range [][]float32{{0, .25, 1, .5, -.1, 1}, {0, .25, 1, .5, 1.1, 1}, {0, .25, 1, .5, float32(math.NaN()), 1}, {0, .25, 1, .5, float32(math.Inf(1)), 1}} {
		bad := maskNPY(header, values)
		if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(bad), int64(len(bad)), maskSHA(bad), 3, 2); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("invalid probability accepted", err)
		}
	}
	for _, badHeader := range []string{
		"{'descr':'<f8','fortran_order':False,'shape':(2,3)}\n",
		"{'descr':'<f4','fortran_order':True,'shape':(2,3)}\n",
		"{'descr':'<f4','fortran_order':False,'shape':(3,2)}\n",
		"{'descr':'<f4','fortran_order':False,'shape':(2,3,1)}\n",
		"{'descr':'<f4','fortran_order':False,'shape':(2,3),'shape':(2,3)}\n",
		"{'descr':'<f4','fortran_order':False,'shape':(2,3),'extra':True}\n",
		"{'descr':'|O','fortran_order':False,'shape':(2,3)}\n",
		"{'descr':'<f4','fortran_order':False,'shape':__import__('os')}\n",
	} {
		bad := maskNPY(badHeader, []float32{0, 0, 0, 0, 0, 0})
		if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(bad), int64(len(bad)), maskSHA(bad), 3, 2); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("invalid header accepted", badHeader, err)
		}
	}
	for _, bad := range [][]byte{valid[:len(valid)-1], append(append([]byte(nil), valid...), 0)} {
		if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(bad), int64(len(valid)), maskSHA(valid), 3, 2); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("length mismatch accepted", err)
		}
	}
	if err := VerifyUserMaskNPY(context.Background(), bytes.NewReader(valid), int64(len(valid)), maskSHA(nil), 3, 2); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("SHA mismatch accepted", err)
	}
}

type maskFailedReader struct{ err error }

func (r maskFailedReader) Read([]byte) (int, error) { return 0, r.err }
func TestUserMaskNPYCancellationAndTransportFailure(t *testing.T) {
	sentinel := errors.New("object read unavailable")
	if err := VerifyUserMaskNPY(context.Background(), maskFailedReader{sentinel}, 100, maskSHA(nil), 1, 1); !errors.Is(err, sentinel) {
		t.Fatal("transport error lost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyUserMaskNPY(ctx, maskFailedReader{sentinel}, 100, maskSHA(nil), 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
