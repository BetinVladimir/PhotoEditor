// Package timeline implements the video timeline document: time arithmetic,
// canonical form, validation (VT-01…VT-10) and edit commands.
package timeline

import (
	"errors"
	"math"
)

// TicksPerSecond is the timebase of every timeline document.
const TicksPerSecond int64 = 705_600_000

var (
	ErrFPSUnsupported = errors.New("FPS_UNSUPPORTED")
	ErrFrameNegative  = errors.New("FRAME_NEGATIVE")
	ErrTicksNegative  = errors.New("TICKS_NEGATIVE")
)

// FrameTicks is FN-T-01: ticks per frame, FPS_UNSUPPORTED when the rate does not divide the timebase.
func FrameTicks(num, den int64) (int64, error) {
	if num <= 0 || den <= 0 || den > math.MaxInt64/TicksPerSecond {
		return 0, ErrFPSUnsupported
	}
	v := TicksPerSecond * den
	if v%num != 0 {
		return 0, ErrFPSUnsupported
	}
	return v / num, nil
}

// FramesToTicks is FN-T-02.
func FramesToTicks(num, den, frame int64) (int64, error) {
	if frame < 0 {
		return 0, ErrFrameNegative
	}
	ft, err := FrameTicks(num, den)
	if err != nil {
		return 0, err
	}
	if frame > math.MaxInt64/ft {
		return 0, errors.New("TICKS_OVERFLOW")
	}
	return frame * ft, nil
}

// TicksToFrame is FN-T-03; round selects (ticks+FT/2)/FT instead of floor.
func TicksToFrame(num, den, ticks int64, round bool) (int64, error) {
	if ticks < 0 {
		return 0, ErrTicksNegative
	}
	ft, err := FrameTicks(num, den)
	if err != nil {
		return 0, err
	}
	if round {
		return (ticks + ft/2) / ft, nil
	}
	return ticks / ft, nil
}
