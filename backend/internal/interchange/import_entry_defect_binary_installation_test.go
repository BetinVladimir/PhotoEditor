package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestEntryBinaryFactorySeparatesInstalledDefectLabels(t *testing.T) {
	factory := SQLImportEntryRecipeAuthorityFactory{Codecs: ImportRecipeCodecDependencies{DefectCandidates: &importDefectCandidatesQualifierStub{}}}
	installed, err := factory.BuildImportEntryBinaryRemapper(context.Background(), uuid.New(), nil, remapArchiveStub{}, &importRecipePorts{}, &importMasterReaderStub{}, &importCatalogStub{}, &importReaderStub{}, PreparedImportArtifactPublisher{Store:SQLPreparedImportArtifactStore{}})
	if err != nil {
		t.Fatal(err)
	}
	router, ok := installed.(ImportBinaryCodecRouter)
	if !ok || router.Masks == nil || router.DefectLabels == nil || router.LUT != nil {
		t.Fatal("typed defect binary route missing")
	}
	without := SQLImportEntryRecipeAuthorityFactory{}
	installed, err = without.BuildImportEntryBinaryRemapper(context.Background(), uuid.New(), nil, remapArchiveStub{}, &importRecipePorts{}, &importMasterReaderStub{}, &importCatalogStub{}, &importReaderStub{}, PreparedImportArtifactPublisher{Store:SQLPreparedImportArtifactStore{}})
	if err != nil {
		t.Fatal(err)
	}
	router, ok = installed.(ImportBinaryCodecRouter)
	if !ok || router.DefectLabels != nil {
		t.Fatal("unqualified defect labels installed")
	}
}
