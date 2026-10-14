package objectstore

import (
 "strings"
 "testing"

 "github.com/google/uuid"
)

func TestInterchangeGenerationKeySeparatesAttempts(t *testing.T) {
 tenant, upload, first, second := uuid.New(), uuid.New(), uuid.New(), uuid.New()
 a, err := InterchangeGenerationKey(tenant, upload, first)
 if err != nil { t.Fatal(err) }
 b, err := InterchangeGenerationKey(tenant, upload, second)
 if err != nil || a == b { t.Fatal("generation keys collided", err) }
 legacy, _ := InterchangeUploadKey(tenant, upload)
 if a == legacy || validInterchangeUploadKey(a) { t.Fatal("generation bypassed legacy key boundary") }
 gotTenant, gotUpload, gotGeneration, err := ParseInterchangeGenerationKey(a)
 if err != nil || gotTenant != tenant || gotUpload != upload || gotGeneration != first { t.Fatal("generation identity changed", err) }
}

func TestInterchangeGenerationKeyRejectsForeignOrMalformedIdentity(t *testing.T) {
 tenant, upload, generation := uuid.New(), uuid.New(), uuid.New()
 key, _ := InterchangeGenerationKey(tenant, upload, generation)
 for _, ids := range [][3]uuid.UUID{{uuid.Nil, upload, generation}, {tenant, uuid.Nil, generation}, {tenant, upload, uuid.Nil}} {
  if _, err := InterchangeGenerationKey(ids[0], ids[1], ids[2]); err != ErrInvalid { t.Fatal("accepted nil identity", ids, err) }
 }
 bad := []string{key + "/extra", strings.ToUpper(key), strings.Replace(key, "/generations/", "/generation/", 1), strings.Replace(key, tenant.String(), uuid.NewString(), 1), strings.Replace(key, generation.String(), "not-a-uuid", 1)}
 // A different valid tenant is a valid key, but it must parse to that tenant
 // so callers can compare against their authenticated SQL scope.
 foreign := bad[3]
 bad = append(bad[:3], bad[4:]...)
 for _, value := range bad {
  if _, _, _, err := ParseInterchangeGenerationKey(value); err != ErrInvalid { t.Fatal("accepted malformed key", value, err) }
 }
 gotTenant, _, _, err := ParseInterchangeGenerationKey(foreign)
 if err != nil || gotTenant == tenant { t.Fatal("foreign tenant identity hidden", err) }
}
