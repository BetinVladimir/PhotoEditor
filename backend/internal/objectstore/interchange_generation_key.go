package objectstore

import (
 "strings"

 "github.com/google/uuid"
)

// InterchangeGenerationKey gives each archive Begin attempt a distinct S3 key.
// The legacy InterchangeUploadKey is intentionally unchanged for existing
// uploads; generation keys require a separately authorized ledger path.
func InterchangeGenerationKey(tenant, upload, generation uuid.UUID) (string, error) {
 if tenant == uuid.Nil || upload == uuid.Nil || generation == uuid.Nil {
  return "", ErrInvalid
 }
 return "tenants/" + tenant.String() + "/interchange-uploads/" + upload.String() + "/generations/" + generation.String() + "/archive.zip", nil
}

// ParseInterchangeGenerationKey accepts only the canonical namespace and UUID
// spelling, so a parsed key can be compared directly to a frozen SQL identity.
func ParseInterchangeGenerationKey(key string) (tenant, upload, generation uuid.UUID, err error) {
 parts := strings.Split(key, "/")
 if len(parts) != 7 || parts[0] != "tenants" || parts[2] != "interchange-uploads" || parts[4] != "generations" || parts[6] != "archive.zip" {
  return uuid.Nil, uuid.Nil, uuid.Nil, ErrInvalid
 }
 tenant, err = uuid.Parse(parts[1]); if err != nil { return uuid.Nil, uuid.Nil, uuid.Nil, ErrInvalid }
 upload, err = uuid.Parse(parts[3]); if err != nil { return uuid.Nil, uuid.Nil, uuid.Nil, ErrInvalid }
 generation, err = uuid.Parse(parts[5]); if err != nil { return uuid.Nil, uuid.Nil, uuid.Nil, ErrInvalid }
 expected, err := InterchangeGenerationKey(tenant, upload, generation)
 if err != nil || key != expected { return uuid.Nil, uuid.Nil, uuid.Nil, ErrInvalid }
 return tenant, upload, generation, nil
}

// Archive operations may address either an existing legacy upload or a
// generation that the application has independently authorized in SQL.
func validInterchangeArchiveKey(key string) bool {
 if validInterchangeUploadKey(key) { return true }
 _, _, _, err := ParseInterchangeGenerationKey(key)
 return err == nil
}
