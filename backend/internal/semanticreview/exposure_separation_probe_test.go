package semanticreview

import (
	"bytes"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/objectstore"
)

var separationProbeError = errors.New("controlled separation refusal")

type separationProbeQuery struct {
	db                       ApprovedProtectionQueryer
	calls, failAt, versionAt int
}
type separationProbeVersionRow struct{ pgx.Row }

func (r separationProbeVersionRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	*dest[6].(*string) = "changed-version"
	return nil
}

type separationProbeDeniedRow struct{}

func (separationProbeDeniedRow) Scan(...any) error { return separationProbeError }
func (q *separationProbeQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	q.calls++
	if q.calls == q.failAt {
		return separationProbeDeniedRow{}
	}
	row := q.db.QueryRow(ctx, sql, args...)
	if q.calls == q.versionAt {
		return separationProbeVersionRow{row}
	}
	return row
}

type separationProbeStore struct {
	store                                 planObjectStore
	opened, closed                        int
	failAt, nilAt, corruptAt, closeFailAt int
	cancel                                context.CancelFunc
}
type separationProbeStream struct {
	io.ReadCloser
	store   *separationProbeStore
	ordinal int
}

func (r *separationProbeStream) Close() error {
	r.store.closed++
	err := r.ReadCloser.Close()
	if r.store.cancel != nil && r.ordinal == 1 {
		r.store.cancel()
	}
	if r.ordinal == r.store.closeFailAt {
		return separationProbeError
	}
	return err
}
func (s *separationProbeStore) Open(ctx context.Context, o objectstore.Object) (io.ReadCloser, error) {
	s.opened++
	ordinal := s.opened
	if ordinal == s.failAt {
		return nil, separationProbeError
	}
	if ordinal == s.nilAt {
		return nil, nil
	}
	var reader io.ReadCloser
	var err error
	if ordinal == s.corruptAt {
		reader = io.NopCloser(bytes.NewReader([]byte("corrupt")))
	} else {
		reader, err = s.store.Open(ctx, o)
	}
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, nil
	}
	return &separationProbeStream{reader, s, ordinal}, nil
}
