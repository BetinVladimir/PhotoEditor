package artifactjson

import (
	"github.com/google/uuid"
	"testing"
)

func TestDeliveryDocumentPurposeRequiresBoundedNameAndLease(t *testing.T) {
	for _, purpose := range []string{"v30_delivery_assembly", "v30_delivery_slots_000", "v30_delivery_slots_159"} {
		scope := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: purpose, AssemblyLease: uuid.New()}
		if _, err := DocumentKey(scope, uuid.New()); err != nil {
			t.Fatal(purpose, err)
		}
		scope.AssemblyLease = uuid.Nil
		if _, err := DocumentKey(scope, uuid.New()); err == nil {
			t.Fatal("unfenced delivery purpose", purpose)
		}
	}
	for _, purpose := range []string{"v30_delivery_slots_160", "v30_delivery_slots_-01", "v30_delivery_slots_1", "v30_delivery_slots_0000", "v30_delivery_slots_00x", "v30_delivery_slots_../", "v30_delivery_assembly_other"} {
		scope := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: purpose, AssemblyLease: uuid.New()}
		if _, err := DocumentKey(scope, uuid.New()); err == nil {
			t.Fatal("unknown delivery purpose", purpose)
		}
	}
}
