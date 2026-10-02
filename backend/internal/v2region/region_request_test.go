package v2region

import (
	"strings"
	"testing"
)

const regionBody = `{"version_id":"11111111-1111-4111-8111-111111111111","rect":{"x0":0,"y0":0,"x1":1,"y1":1},"recipe":{"artifact_id":"22222222-2222-4222-8222-222222222222","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"max_edge":1024}`

func TestDecodeRegionPreviewContract(t *testing.T) {
	req, err := DecodeRegionPreviewRequest([]byte(regionBody))
	if err != nil || req.MaxEdge != 1024 || req.Rect.X0 != 0 {
		t.Fatalf("valid request rejected: %v", err)
	}
	for _, body := range []string{
		strings.Replace(regionBody, `"x0":0,`, "", 1),
		strings.Replace(regionBody, `"x0":0`, `"x0":null`, 1),
		strings.Replace(regionBody, `"max_edge":1024`, `"max_edge":1024,"extra":true`, 1),
		strings.Replace(regionBody, `"x1":1`, `"x1":1,"x1":0`, 1),
		strings.Replace(regionBody, `"max_edge":1024`, `"max_edge":1025`, 1),
		strings.Replace(regionBody, strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
		regionBody + ` {}`, strings.Repeat(" ", 16385),
	} {
		if _, err := DecodeRegionPreviewRequest([]byte(body)); err != ErrRegionInvalid {
			t.Fatalf("invalid request admitted: %v", err)
		}
	}
}
