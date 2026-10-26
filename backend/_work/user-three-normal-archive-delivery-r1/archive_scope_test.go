package main

import (
	"github.com/google/uuid"
	"testing"
)

func TestExactArchiveTaskRunPairs(t *testing.T) {
	if archiveTaskForRun(uuid.MustParse("d8108e3d-9609-4f74-b84a-b1b708bba11b")) != uuid.MustParse("1e66b2eb-536b-579a-a940-397e6506d870") {
		t.Fatal("JPEG binding")
	}
	if archiveTaskForRun(uuid.MustParse("e991510e-6de9-4aa3-ad25-2e9192a49f6d")) != uuid.MustParse("aa4e6164-4366-5cee-a1d3-cafaae8333f0") {
		t.Fatal("TIFF binding")
	}
	if archiveTaskForRun(uuid.New()) != uuid.Nil {
		t.Fatal("foreign run admitted")
	}
}
func TestOnlyNormalArchivingFinalizationPhase(t *testing.T) {
	if !advancePhase("archiving") || advancePhase("packaging") || advancePhase("encoding") {
		t.Fatal("closed finalization phase")
	}
}
