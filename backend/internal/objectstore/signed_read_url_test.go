package objectstore

import "testing"

func TestSignedReadURLAcceptsOnlyVersionedSecureOrLoopbackTransport(t *testing.T) {
	for _, raw := range []string{
		"https://objects.example/read?versionId=v1&X-Amz-Signature=opaque",
		"http://localhost:9000/read?versionId=v1",
		"http://127.0.0.2:9000/read?versionId=v1",
		"http://[::1]:9000/read?versionId=v1",
	} {
		if !ValidSignedReadURL(raw, "v1") {
			t.Fatalf("valid signed transport refused: %s", raw)
		}
	}
	for _, raw := range []string{
		"http://objects.example/read?versionId=v1",
		"http://localhost.evil.example/read?versionId=v1",
		"http://192.168.1.2/read?versionId=v1",
		"https://objects.example/read?versionId=v2",
		"https://objects.example/read?versionId=v1&versionId=v2",
		"https://objects.example/read?versionId=v1&%GG=bad",
		"https://user@objects.example/read?versionId=v1",
		"https://objects.example/read?versionId=v1#fragment",
		"file:///read?versionId=v1",
	} {
		if ValidSignedReadURL(raw, "v1") {
			t.Fatalf("unsafe or unbound signed transport accepted: %s", raw)
		}
	}
}
