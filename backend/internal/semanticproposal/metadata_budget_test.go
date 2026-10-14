package semanticproposal

import (
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMetadataBudgetAtomicLimitAndOverflow(t *testing.T) {
	used := int64(5)
	if err := chargeMetadata(&used, 10, 2, 3); err != nil || used != 10 {
		t.Fatal("exact boundary", used, err)
	}
	if err := chargeMetadata(&used, 10, 1); err != v2input.ErrInvalid || used != 10 {
		t.Fatal("limit bypass", used, err)
	}
	used = 1
	if err := chargeMetadata(&used, 10, 2, 8); err != v2input.ErrInvalid || used != 1 {
		t.Fatal("partial charge", used, err)
	}
	used = 9223372036854775806
	if err := chargeMetadata(&used, 9223372036854775807, 2); err != v2input.ErrInvalid {
		t.Fatal("overflow accepted")
	}
	if validMetadataBudget(TransferPreparation{RAM: 1024, MetadataBytes: 129}) || validMetadataBudget(TransferPreparation{RAM: 1024}) || !validMetadataBudget(TransferPreparation{RAM: 1024, MetadataBytes: 128}) {
		t.Fatal("metadata headroom invalid")
	}
}
