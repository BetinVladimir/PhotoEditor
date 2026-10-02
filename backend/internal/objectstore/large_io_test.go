package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"
	"time"
)

func TestIntegrationLargeImmutableIO(t *testing.T) {
	if os.Getenv("PHOTO_RUN_S3_LARGE_IO") != "1" {
		t.Skip("opt-in >4GiB real S3 IO")
	}
	s := testStore(t)
	const size int64 = 1<<32 + 1<<20 + 17
	// Use a fixed read buffer for identical fixture boundaries on both passes.
	block := make([]byte, 1<<20)
	for i := range block {
		block[i] = byte(i % 251)
	}
	hash := sha256.New()
	for remaining := size; remaining > 0; {
		n := int64(len(block))
		if n > remaining {
			n = remaining
		}
		hash.Write(block[:n])
		remaining -= n
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// Reader boundaries vary with SDK requests, so repeat the block with a cursor.
	body := &repeatingLargeFixture{remaining: size, block: block}
	t.Logf("uploading %d bytes", size)
	object, err := s.PutImmutable(ctx, "large/fixture.bin", body, size, digest, "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	if object.Bytes != size || object.VersionID == "" || object.VersionID == "null" {
		t.Fatal("missing immutable version")
	}
	t.Log("reading full pinned object")
	reader, err := s.Open(ctx, object)
	if err != nil {
		t.Fatal(err)
	}
	read, err := io.CopyBuffer(io.Discard, reader, make([]byte, 1<<20))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || read != size {
		t.Fatalf("read=%d error=%v close=%v", read, err, closeErr)
	}
	t.Logf("verified %d bytes with SHA %s", read, digest)
}

type repeatingLargeFixture struct {
	remaining int64
	block     []byte
	offset    int
}

func (r *repeatingLargeFixture) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	written := 0
	for written < n {
		copied := copy(p[written:n], r.block[r.offset:])
		written += copied
		r.offset = (r.offset + copied) % len(r.block)
	}
	r.remaining -= int64(n)
	return n, nil
}
