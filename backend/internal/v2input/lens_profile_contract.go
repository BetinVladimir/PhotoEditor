package v2input

import (
	"bytes"
	"encoding/json"
	"io"
	"photoeditor/backend/internal/canonical"
	"unicode/utf8"
)

type LensProfileDocument struct {
	SchemaVersion int       `json:"schema_version"`
	CameraModel   string    `json:"camera_model"`
	LensModel     string    `json:"lens_model"`
	FocalMM       float64   `json:"focal_mm"`
	Aperture      float64   `json:"aperture"`
	CX            float64   `json:"cx"`
	CY            float64   `json:"cy"`
	K             []float64 `json:"k"`
	P             []float64 `json:"p"`
	Vignette      []float64 `json:"vignette"`
	CARedScale    float64   `json:"ca_red_scale"`
	CABlueScale   float64   `json:"ca_blue_scale"`
	Qualification Ref       `json:"qualification"`
}

func ParseLensProfile(raw []byte) (LensProfileDocument, error) {
	var result LensProfileDocument
	if len(raw) < 2 || len(raw) > 1048576 {
		return result, ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return result, ErrInvalid
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || len(keys) != 13 {
		return result, ErrInvalid
	}
	for _, key := range []string{"schema_version", "camera_model", "lens_model", "focal_mm", "aperture", "cx", "cy", "k", "p", "vignette", "ca_red_scale", "ca_blue_scale", "qualification"} {
		value, ok := keys[key]
		if !ok || string(value) == "null" {
			return result, ErrInvalid
		}
	}

	for _, key := range []string{"k", "p", "vignette"} {
		var coefficients []*float64
		if json.Unmarshal(keys[key], &coefficients) != nil {
			return result, ErrInvalid
		}
		for _, value := range coefficients {
			if value == nil {
				return result, ErrInvalid
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF {
		return result, ErrInvalid
	}
	if result.SchemaVersion != 1 || !utf8.ValidString(result.CameraModel) || !utf8.ValidString(result.LensModel) ||
		utf8.RuneCountInString(result.CameraModel) < 1 || utf8.RuneCountInString(result.CameraModel) > 120 ||
		utf8.RuneCountInString(result.LensModel) < 1 || utf8.RuneCountInString(result.LensModel) > 120 ||
		!finiteRange(result.FocalMM, 1, 2000) || !finiteRange(result.Aperture, .5, 128) || !finiteRange(result.CX, 0, 1) || !finiteRange(result.CY, 0, 1) ||
		!finiteRange(result.CARedScale, .99, 1.01) || !finiteRange(result.CABlueScale, .99, 1.01) || !validRef(result.Qualification) ||
		len(result.K) != 3 || len(result.P) != 2 || len(result.Vignette) != 3 {
		return result, ErrInvalid
	}
	for _, series := range []struct {
		values []float64
		bound  float64
	}{{result.K, 2}, {result.P, .1}, {result.Vignette, 2}} {
		for _, value := range series.values {
			if !finiteRange(value, -series.bound, series.bound) {
				return result, ErrInvalid
			}
		}
	}
	return result, nil
}
