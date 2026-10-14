package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestPreparedLUTResolverRejectsMissingNativeAndWitnessAuthority(t *testing.T) {
	if _, err := NewSQLPreparedImportLUTResolver(context.Background(), SQLPreparedImportCatalog{}, nil, nil, nil, nil, nil, "", 1024); err == nil {
		t.Fatal("unqualified LUT resolver installed")
	}
	if _, err := (SQLPreparedImportLUTGeometry{}).ResolveExportArrayGeometry(context.Background(), v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("lut"))}, Contract: "AbsoluteLUTArray"}); err == nil {
		t.Fatal("unwitnessed LUT geometry accepted")
	}
}
