package semanticreview

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func TestReviewKindRequiresExplicitInstallation(t *testing.T) {
	reader := func(context.Context, PlanSource) ([]byte, error) { t.Fatal("unexpected IO"); return nil, nil }
	for _, test := range []struct {
		db   *pgxpool.Pool
		read PlanObjectReader
	}{{nil, nil}, {nil, reader}, {new(pgxpool.Pool), nil}} {
		if _, err := SQLReviewKindReader(test.db, test.read); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
}
