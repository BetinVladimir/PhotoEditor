package review

import (
	"crypto/sha256"
	"encoding/hex"

	"photoeditor/backend/internal/objectstore"
)

func previewObjectKeySHA(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

func previewLinkObjectMatches(link CandidatePreviewLink, object objectstore.Object, ref ArtifactRef, version string) bool {
	return object.Key != "" && object.VersionID == version && object.SHA256 == ref.SHA256 &&
		link.Artifact == ref && link.Bytes == object.Bytes && link.MediaType == object.ContentType
}
