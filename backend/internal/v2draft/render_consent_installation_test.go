package v2draft

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type consentInstallationRow struct { valid bool; err error }
func (r consentInstallationRow) Scan(dest ...any) error {
	if r.err != nil { return r.err }
	*(dest[0].(*bool)) = r.valid
	return nil
}
type consentInstallationDB struct { row consentInstallationRow; calls int }
func (d *consentInstallationDB) QueryRow(context.Context, string, ...any) pgx.Row {
	d.calls++
	return d.row
}

func TestRenderConsentInstallationRefusesMissingInvalidAndQueryFailure(t *testing.T) {
	if RequireRenderConsentDiagnostic(context.Background(),nil)==nil { t.Fatal("nil database accepted") }
	failure:=errors.New("database failure")
	for _, row:=range []consentInstallationRow{{valid:true},{valid:false},{err:failure}} {
		db:=&consentInstallationDB{row:row}
		err:=RequireRenderConsentDiagnostic(context.Background(),db)
		if (err==nil)!=(row.valid && row.err==nil) || db.calls!=1 { t.Fatal("installation outcome",err,db.calls) }
		if row.err!=nil && !errors.Is(err,row.err) { t.Fatal("query failure masked",err) }
	}
}
