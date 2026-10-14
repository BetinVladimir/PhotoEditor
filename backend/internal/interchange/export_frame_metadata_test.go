package interchange

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestExportFrameMetadataFlagMappingAndPreflight(t *testing.T) {
	for flag, want := range map[string]string{"pick": "picked", "reject": "rejected", "none": "unmarked"} {
		got, err := exportPickFromCollectionFlag(flag)
		if err != nil || got != want {
			t.Fatal(flag, got, err)
		}
	}
	for _, flag := range []string{"", "picked", "unknown"} {
		if _, err := exportPickFromCollectionFlag(flag); err == nil {
			t.Fatal("invented metadata", flag)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadExportFrameMetadata(ctx, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, v2input.SessionSnapshotItem{}); err != context.Canceled {
		t.Fatal("cancellation", err)
	}
	if _, err := ReadExportFrameMetadata(context.Background(), nil, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), v2input.SessionSnapshotItem{}); err != v2input.ErrInvalid {
		t.Fatal("missing scope", err)
	}
	if _, err := (SQLExportFrameMetadataSource{}).Read(context.Background(), v2input.SessionSnapshotItem{}); err == nil {
		t.Fatal("missing database")
	}
}

type exportMetadataTx struct {
	calls  int
	rating int
	flag   string
	label  *string
	err    error
	args   []any
}

func (s *exportMetadataTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	s.calls++
	s.args = args
	return ledgerRow(func(dest ...any) error {
		if s.err != nil {
			return s.err
		}
		*dest[0].(*int) = s.rating
		*dest[1].(*string) = s.flag
		*dest[2].(**string) = s.label
		return nil
	})
}
func TestExportFrameMetadataReadsExactScopeAndRejectsBadCatalogValues(t *testing.T) {
	item := v2input.SessionSnapshotItem{Ordinal: 3, Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("source"))}}}
	tid, project, snapshot := uuid.New(), uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, mode := range []string{"valid", "label", "invalid_label", "negative", "too_high", "invalid_flag", "missing", "database"} {
		tx := &exportMetadataTx{rating: 4, flag: "pick"}
		cause := errors.New("database unavailable")
		switch mode {
		case "label":
			value := "Blue"
			tx.label = &value
		case "invalid_label":
			value := "Custom"
			tx.label = &value
		case "negative":
			tx.rating = -1
		case "too_high":
			tx.rating = 6
		case "invalid_flag":
			tx.flag = "picked"
		case "missing":
			tx.err = pgx.ErrNoRows
		case "database":
			tx.err = cause
		}
		got, err := ReadExportFrameMetadata(context.Background(), tx, who, tid, project, snapshot, item)
		if mode == "valid" || mode == "label" {
			if err != nil || got.Rating != 4 || got.Pick != "picked" || got.ExternalID != item.Binding.AssetVersionID.String() || (mode == "label" && (got.Label == nil || *got.Label != "Blue")) {
				t.Fatal("metadata", err)
			}
		} else if err == nil || got.ExternalID != "" {
			t.Fatal("invented catalog metadata", mode, err)
		}
		if tx.calls != 1 || tx.args[0] != tid || tx.args[1] != snapshot || tx.args[4] != item.Binding.AssetVersionID || tx.args[6] != item.Binding.Source.SHA256 {
			t.Fatal("wrong source binding")
		}
		if mode == "database" && !errors.Is(err, cause) {
			t.Fatal("database cause lost")
		}
	}
}
