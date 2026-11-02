// Package quota holds the pure upload-plan and storage-estimate arithmetic (FN-Q-01…05).
package quota

import (
	"errors"
	"math"
	"math/big"
)

const (
	mib = int64(1) << 20
	gib = int64(1) << 30
	// minPartBytes is the smallest multipart part the plan will use.
	minPartBytes = 32 * mib
	// maxParts is the object-store multipart limit the 9000 divisor stays safely under.
	partsDivisor = 9000
)

var (
	ErrBytesInvalid = errors.New("BYTES_INVALID")
	ErrTooLarge     = errors.New("VIDEO_TOO_LARGE")
	ErrOverflow     = errors.New("ESTIMATE_OVERFLOW")
)

func ceilDiv(a, b int64) int64 { return (a + b - 1) / b }

// PartBytes is FN-Q-01. maxBytes is the configured upload limit (open question Q-02);
// the caller must pass it explicitly, there is no built-in default.
func PartBytes(bytes, maxBytes int64) (int64, error) {
	if bytes <= 0 {
		return 0, ErrBytesInvalid
	}
	if bytes > maxBytes {
		return 0, ErrTooLarge
	}
	raw := ceilDiv(bytes, partsDivisor)
	r := ceilDiv(raw, mib) * mib
	return max(minPartBytes, r), nil
}

// PartsTotal is FN-Q-02.
func PartsTotal(bytes, maxBytes int64) (int64, error) {
	pb, err := PartBytes(bytes, maxBytes)
	if err != nil {
		return 0, err
	}
	return ceilDiv(bytes, pb), nil
}

// DerivativeEstimate is FN-Q-03: min(bytes*15/100, 4 GiB) + 128 MiB.
func DerivativeEstimate(bytes int64) (int64, error) {
	if bytes < 0 {
		return 0, ErrBytesInvalid
	}
	e := new(big.Int).Mul(big.NewInt(bytes), big.NewInt(15))
	e.Quo(e, big.NewInt(100))
	if cap := big.NewInt(4 * gib); e.Cmp(cap) > 0 {
		e = cap
	}
	return toInt64(e.Add(e, big.NewInt(128*mib)))
}

// RenderEstimate is FN-Q-04: ceil(durationTicks*(maxrate+audio)*105 / (8*100*TPS)) + 64 MiB, in arbitrary precision.
func RenderEstimate(durationTicks, maxrateBps, audioBps int64) (int64, error) {
	if durationTicks < 0 || maxrateBps < 0 || audioBps < 0 {
		return 0, ErrBytesInvalid
	}
	num := new(big.Int).Mul(big.NewInt(durationTicks), new(big.Int).Add(big.NewInt(maxrateBps), big.NewInt(audioBps)))
	num.Mul(num, big.NewInt(105))
	den := big.NewInt(8 * 100 * 705_600_000)
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	if r.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}
	return toInt64(q.Add(q, big.NewInt(64*mib)))
}

// Admit is FN-Q-05: used+reserved+request <= limit. A zero limit admits only a zero request.
func Admit(used, reserved, limit, request int64) bool {
	if used < 0 || reserved < 0 || limit < 0 || request < 0 {
		return false
	}
	sum := new(big.Int).Add(big.NewInt(used), big.NewInt(reserved))
	sum.Add(sum, big.NewInt(request))
	return sum.Cmp(big.NewInt(limit)) <= 0
}

func toInt64(v *big.Int) (int64, error) {
	if !v.IsInt64() || v.Int64() > math.MaxInt64 {
		return 0, ErrOverflow
	}
	return v.Int64(), nil
}
