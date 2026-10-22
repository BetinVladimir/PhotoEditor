package semanticreview

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type retouchAdmissionRoleRow struct { ready bool; err error }

func (r retouchAdmissionRoleRow) Scan(dest ...any) error {
	if r.err != nil { return r.err }
	*dest[0].(*bool) = r.ready
	return nil
}

type retouchAdmissionRoleQuery struct { row retouchAdmissionRoleRow; calls int }

func (q *retouchAdmissionRoleQuery) QueryRow(context.Context, string, ...any) pgx.Row {
	q.calls++
	return q.row
}

func TestShownRetouchAdmissionRoleRefusesMissingAuthority(t *testing.T) {
	failure := errors.New("catalog query failed")
	for _, tc := range []struct { name string; row retouchAdmissionRoleRow; want error }{
		{"qualified", retouchAdmissionRoleRow{ready: true}, nil},
		{"missing_commit_authority", retouchAdmissionRoleRow{}, ErrUnavailable},
		{"catalog_failure", retouchAdmissionRoleRow{err: failure}, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := &retouchAdmissionRoleQuery{row: tc.row}
			err := verifyShownRetouchAdmissionRole(context.Background(), query)
			if !errors.Is(err, tc.want) || query.calls != 1 { t.Fatal("role gate lost refusal", err) }
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	query := &retouchAdmissionRoleQuery{row: retouchAdmissionRoleRow{ready: true}}
	if err := verifyShownRetouchAdmissionRole(ctx, query); !errors.Is(err, context.Canceled) || query.calls != 0 {
		t.Fatal("canceled role gate queried dependencies", err)
	}
	if err := verifyShownRetouchAdmissionRole(context.Background(), nil); !errors.Is(err, ErrUnavailable) { t.Fatal(err) }
}
