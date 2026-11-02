package timeline

import "photoeditor/backend/internal/canonical"

// Canonicalize is FN-T-11: RFC 8785 bytes of the document.
func Canonicalize(d Doc) ([]byte, error) { return canonical.Marshal(d) }

// DocumentSHA256 is FN-T-11: lowercase hex SHA-256 of the canonical bytes.
func DocumentSHA256(d Doc) (string, error) { return canonical.Hash(d) }
