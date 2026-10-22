package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// verifyAuthoredSegmentStream consumes the exact original object version with a
// fixed 64KiB buffer. Even after canonical adoption, a fresh read must discover
// corruption/truncation/extra bytes; metadata alone is not physical evidence.
func verifyAuthoredSegmentStream(ctx context.Context, objects v2input.SessionAnalysisObjectReader, obj objectstore.Object, ref v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if objects == nil || !validLayerArtifactRef(ref) || obj.SHA256 != ref.SHA256 || obj.Bytes < 1 || obj.Bytes > 2400004096 || obj.VersionID == "" || obj.VersionID == "null" {
		return v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		return err
	}
	hash := sha256.New()
	buf := make([]byte, 64*1024)
	var total int64
	empty := 0
	var readErr error
	for {
		if readErr = ctx.Err(); readErr != nil {
			break
		}
		limit := int64(len(buf))
		if remain := obj.Bytes + 1 - total; remain < limit {
			limit = remain
		}
		if limit < 1 {
			readErr = v2input.ErrInvalid
			break
		}
		n, e := reader.Read(buf[:int(limit)])
		if n < 0 || n > int(limit) {
			readErr = v2input.ErrInvalid
			break
		}
		if n > 0 {
			total += int64(n)
			_, _ = hash.Write(buf[:n])
			empty = 0
		} else {
			empty++
		}
		if total > obj.Bytes {
			readErr = v2input.ErrInvalid
			break
		}
		if e != nil {
			if e != io.EOF {
				readErr = e
			}
			break
		}
		if empty >= 100 {
			readErr = io.ErrNoProgress
			break
		}
	}
	closeErr := reader.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if total != obj.Bytes || hex.EncodeToString(hash.Sum(nil)) != ref.SHA256 {
		return v2input.ErrInvalid
	}
	return nil
}
