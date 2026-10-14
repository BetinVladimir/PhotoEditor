package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeVersionParamsRejectInvalidConfigurationBeforeSourceIO(t *testing.T) {
	reader := SQLTransferSourceReader{DB: &pgxpool.Pool{}, Objects: nativePublicationNoIO{}, Bucket: "test", ProjectID: uuid.New()}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, document := uuid.New(), uuid.New()
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	for _, score := range []float64{math.NaN(), math.Inf(1), .49, 1} {
		if _, err := reader.publishSourceDetectorVersionParams(context.Background(), who, tenant, binding, TransferColorProfile{}, v2draft.Publisher{}, document, strings.Repeat("c", 64), score, 10); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("invalid SFD config", err)
		}
	}
	for _, tile := range []int{0, 128, 2048} {
		if _, err := reader.publishSourcePersonVersionParams(context.Background(), who, tenant, binding, TransferColorProfile{}, v2draft.Publisher{}, document, strings.Repeat("c", 64), tile); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("invalid LRASPP config", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.publishSourcePersonVersionParams(ctx, who, tenant, binding, TransferColorProfile{}, v2draft.Publisher{}, document, strings.Repeat("c", 64), 512); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// Poison dependencies make accidentally reaching source IO fail the test.
type nativePublicationNoIO struct{}

func (nativePublicationNoIO) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	panic("native publication preflight reached object IO")
}
