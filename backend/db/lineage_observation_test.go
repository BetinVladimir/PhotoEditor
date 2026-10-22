package migrations

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type lineageTestRow struct {
	values []any
	err    error
}

func (row lineageTestRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("row shape")
	}
	for i, value := range row.values {
		if value == nil {
			return errors.New("NULL value")
		}
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

type lineageTestRows struct {
	pgx.Rows
	values        [][]any
	position      int
	closed        bool
	terminalError error
}

func (rows *lineageTestRows) Next() bool { rows.position++; return rows.position < len(rows.values) }
func (rows *lineageTestRows) Scan(dest ...any) error {
	return (lineageTestRow{values: rows.values[rows.position]}).Scan(dest...)
}
func (rows *lineageTestRows) Err() error { return rows.terminalError }
func (rows *lineageTestRows) Close()     { rows.closed = true }

type lineageTestTransaction struct {
	pgx.Tx
	exec                  []string
	query                 []string
	committed, rolledBack bool
	unsupported           bool
	oversize              bool
	missingLedger         bool
	ambiguousLedger       bool
	functionRows          int
	errorFamily           string
	immediateError        bool
}

func (tx *lineageTestTransaction) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	tx.exec = append(tx.exec, query)
	return pgconn.NewCommandTag("SET"), nil
}
func (tx *lineageTestTransaction) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if query == "SELECT current_setting('server_version_num')" {
		return lineageTestRow{values: []any{"180004"}}
	}
	return lineageTestRow{values: []any{!tx.missingLedger, tx.ambiguousLedger}}
}
func (tx *lineageTestTransaction) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	tx.query = append(tx.query, query)
	rows := &lineageTestRows{position: -1}
	if query == lineageUnsupportedSQL {
		count := int64(0)
		if tx.unsupported {
			count = 1
		}
		rows.values = [][]any{{"pg_operator", count}}
		return rows, nil
	}
	if query == lineagePublicLedgerSQL || query == lineagePhotoLedgerSQL {
		rows.values = [][]any{{"1", "0", true, "2026-10-21 00:00:00"}, {"2", "2637", true, "2026-10-21 01:00:00"}}
		return rows, nil
	}
	for _, projection := range lineageObservationQueries {
		if query == projection.SQL {
			if projection.Family == tx.errorFamily {
				failure := &pgconn.PgError{Code: "22023", Message: "ACL arrays must be one-dimensional"}
				if tx.immediateError {
					return nil, failure
				}
				rows.terminalError = failure
				return rows, nil
			}
			switch projection.Family {
			case "schema", "functions", "relations", "roles", "ledger_schema":
				rows.values = [][]any{{projection.Family + "_identity", strings.Repeat("a", 64)}}
			}
			if projection.Family == "functions" {
				if tx.oversize {
					rows.values = [][]any{{"oversized_function", nil}}
				}
				if tx.functionRows > 0 {
					rows.values = nil
					for i := 0; i < tx.functionRows; i++ {
						rows.values = append(rows.values, []any{string(rune('a' + i)), strings.Repeat("b", 64)})
					}
				}
			}
			return rows, nil
		}
	}
	return nil, errors.New("unknown non-fixed query")
}
func (tx *lineageTestTransaction) Commit(context.Context) error   { tx.committed = true; return nil }
func (tx *lineageTestTransaction) Rollback(context.Context) error { tx.rolledBack = true; return nil }

type lineageTestSource struct {
	tx       *lineageTestTransaction
	options  pgx.TxOptions
	deadline bool
}

func (source *lineageTestSource) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	source.options = options
	_, source.deadline = ctx.Deadline()
	return source.tx, nil
}
func observeLineageFixture(t *testing.T) LineageObservation {
	t.Helper()
	source := &lineageTestSource{tx: &lineageTestTransaction{}}
	result, err := ObserveLineage(context.Background(), source, DefaultLineageObservationLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !source.deadline || source.options.AccessMode != pgx.ReadOnly || source.options.IsoLevel != pgx.RepeatableRead || !source.tx.committed {
		t.Fatal("unsafe observation transaction")
	}
	return result
}
func TestLineageObservationReadsBoundedRepeatableSnapshot(t *testing.T) {
	result := observeLineageFixture(t)
	if !result.Complete || result.FamilyCounts["schema"] != 1 || len(result.Ledger) != 2 || result.ExactLedgerSHA256 == "" {
		t.Fatal("incomplete observation")
	}
	if _, err := ClassifyLineageObservation(result, []LineageSnapshotAuthority{{Identity: "independently-reviewed-snapshot", SemanticSHA256: result.SemanticSHA256}}); err != nil {
		t.Fatal(err)
	}
}
func TestLineageClassificationFailsClosedOnPartialUnknownAndAmbiguousAuthority(t *testing.T) {
	value := observeLineageFixture(t)
	allowed := []LineageSnapshotAuthority{{Identity: "snapshot", SemanticSHA256: value.SemanticSHA256}}
	for _, damage := range []string{"partial", "unknown", "object_hash", "unknown_family", "missing_family", "duplicate_identity", "unsorted", "empty_ledger", "wrong_format", "wrong_coverage", "counts", "invalid_object_hash"} {
		t.Run(damage, func(t *testing.T) {
			changed := value
			changed.Objects = append([]LineageObjectDigest{}, value.Objects...)
			changed.FamilyCounts = map[string]int{}
			for k, v := range value.FamilyCounts {
				changed.FamilyCounts[k] = v
			}
			switch damage {
			case "partial":
				changed.Complete = false
			case "unknown":
				changed.SemanticSHA256 = strings.Repeat("f", 64)
			case "object_hash":
				changed.Objects[0].SHA256 = strings.Repeat("0", 64)
			case "unknown_family":
				changed.Objects[0].Family = "unobserved"
			case "missing_family":
				delete(changed.FamilyCounts, "schema")
			case "duplicate_identity":
				changed.Objects = append(changed.Objects, changed.Objects[len(changed.Objects)-1])
			case "unsorted":
				changed.Objects[0], changed.Objects[1] = changed.Objects[1], changed.Objects[0]
			case "empty_ledger":
				changed.Ledger = nil
			case "wrong_format":
				changed.Format = "future"
			case "wrong_coverage":
				changed.Coverage = "partial"
			case "counts":
				changed.FamilyCounts["functions"]++
			case "invalid_object_hash":
				changed.Objects[0].SHA256 = "not-sha"
			}
			if _, err := ClassifyLineageObservation(changed, allowed); err == nil {
				t.Fatal("invalid observation classified")
			}
		})
	}
	for _, authorities := range [][]LineageSnapshotAuthority{nil, {{Identity: "unknown", SemanticSHA256: strings.Repeat("e", 64)}}, {allowed[0], allowed[0]}, {{Identity: "same", SemanticSHA256: value.SemanticSHA256}, {Identity: "same", SemanticSHA256: strings.Repeat("a", 64)}}, {{Identity: "one", SemanticSHA256: value.SemanticSHA256}, {Identity: "two", SemanticSHA256: value.SemanticSHA256}}} {
		if _, err := ClassifyLineageObservation(value, authorities); err == nil {
			t.Fatal("ambiguous authority accepted")
		}
	}
}
func TestLineageSemanticAndExactLedgerWitnessAreSeparate(t *testing.T) {
	value := observeLineageFixture(t)
	original := value.SemanticSHA256
	value.ExactLedgerSHA256 = strings.Repeat("9", 64)
	hash, err := lineageSemanticHash(value)
	if err != nil || hash != original {
		t.Fatal("physical ledger witness leaked into semantic identity")
	}
	value.Ledger = append([]LineageLedgerStep{}, value.Ledger...)
	value.Ledger[1].Applied = false
	hash, err = lineageSemanticHash(value)
	if err != nil || hash == original {
		t.Fatal("logical ledger history change omitted")
	}
}
func TestLineageObservationUnsupportedOversizedAndMissingLedgerRefuse(t *testing.T) {
	for _, test := range []struct {
		name string
		tx   lineageTestTransaction
	}{{"unsupported", lineageTestTransaction{unsupported: true}}, {"oversized", lineageTestTransaction{oversize: true}}, {"missing_ledger", lineageTestTransaction{missingLedger: true}}, {"ambiguous_ledger", lineageTestTransaction{ambiguousLedger: true}}} {
		t.Run(test.name, func(t *testing.T) {
			tx := test.tx
			value, err := ObserveLineage(context.Background(), &lineageTestSource{tx: &tx}, DefaultLineageObservationLimits())
			if err == nil || value.Complete || tx.committed || !tx.rolledBack {
				t.Fatal("unsafe observation returned usable partial result", err)
			}
		})
	}
}
func TestLineageObservationResourceLimitsRefuse(t *testing.T) {
	for _, limits := range []LineageObservationLimits{{}, {MaxRows: 100001, MaxBytes: 64 << 20, Timeout: time.Second}, {MaxRows: 100, MaxBytes: 65 << 20, Timeout: time.Second}, {MaxRows: 100, MaxBytes: 64 << 20, Timeout: 31 * time.Second}, {MaxRows: 1, MaxBytes: 64 << 20, Timeout: time.Second}, {MaxRows: 100, MaxBytes: 128, Timeout: time.Second}} {
		tx := &lineageTestTransaction{}
		value, err := ObserveLineage(context.Background(), &lineageTestSource{tx: tx}, limits)
		if err == nil || value.Complete || tx.committed {
			t.Fatal("unbounded/partial observation accepted")
		}
	}
}
func TestLineageQueriesAreFixedSymbolicReadOnlyAndObjectBounded(t *testing.T) {
	known := map[string]bool{}
	for _, query := range lineageObservationQueries {
		if known[query.Family] {
			t.Fatal("duplicate family")
		}
		known[query.Family] = true
		if !strings.HasPrefix(query.SQL, "WITH raw AS MATERIALIZED") || !strings.Contains(query.SQL, "octet_length(serialized)<=4194304") || !strings.Contains(query.SQL, "ELSE NULL END AS sha256") {
			t.Fatal("unbounded serialization", query.Family)
		}
		for _, forbidden := range []string{"rolpassword", "pg_authid", "SELECT *", "EXECUTE ", "INSERT INTO ", "UPDATE photo.", "DELETE FROM "} {
			if strings.Contains(query.SQL, forbidden) {
				t.Fatal("unsafe projection", query.Family, forbidden)
			}
		}
	}
	expected := []string{"schema", "functions", "extension_functions", "aggregates", "relations", "columns", "constraints", "indexes", "triggers", "policies", "views", "rules", "sequences", "types", "enum_values", "ranges", "inheritance", "roles", "memberships", "default_acl", "extensions", "role_database_settings", "ledger_schema", "comments"}
	sort.Strings(expected)
	actual := []string{}
	for family := range known {
		actual = append(actual, family)
	}
	sort.Strings(actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("coverage changed without review", actual)
	}
	extension := lineageObservationQueries[2]
	if extension.Family != "extension_functions" || !strings.Contains(extension.SQL, "dependency.refclassid='pg_extension'::regclass") || !strings.Contains(extension.SQL, "n.nspname<>'photo'") {
		t.Fatal("extension function dependency scope lost")
	}
	relations := ""
	for _, query := range lineageObservationQueries {
		if query.Family == "relations" {
			relations = query.SQL
		}
	}
	if !strings.Contains(relations, "'toast_options',toast.reloptions") {
		t.Fatal("TOAST settings omitted")
	}
	if !strings.Contains(lineageObservationQueries[1].SQL, "CASE WHEN p.prokind='a' THEN NULL ELSE pg_get_functiondef(p.oid) END") {
		t.Fatal("aggregates passed to functiondef")
	}
}

func TestLineageACLProjectionNormalizesEveryEmptyArray(t *testing.T) {
	count := 0
	for _, query := range lineageObservationQueries {
		occurrences := strings.Count(query.SQL, "aclexplode(")
		if occurrences == 0 {
			continue
		}
		if strings.Count(query.SQL, "aclexplode(CASE WHEN cardinality(") != occurrences {
			t.Fatal("zero-dimensional ACL reaches aclexplode", query.Family)
		}
		if strings.Count(query.SQL, "THEN NULL::aclitem[] ELSE") != occurrences {
			t.Fatal("empty ACL grants were synthesized", query.Family)
		}
		count += occurrences
	}
	if count != 8 {
		t.Fatal("ACL coverage changed without review", count)
	}
	columns := ""
	for _, query := range lineageObservationQueries {
		if query.Family == "columns" {
			columns = query.SQL
		}
	}
	if !strings.Contains(columns, "'acl_null',a.attacl IS NULL") || !strings.Contains(columns, "cardinality(coalesce(a.attacl,'{}'::aclitem[]))=0") {
		t.Fatal("column rawNULL semantics lost")
	}
	if strings.Contains(columns, "acldefault(") {
		t.Fatal("column NULL ACL gained owner grants")
	}
}

func TestLineageQueryAndStreamingErrorsRetainFamilyAndSQLState(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		tx := &lineageTestTransaction{errorFamily: "columns", immediateError: immediate}
		result, err := ObserveLineage(context.Background(), &lineageTestSource{tx: tx}, DefaultLineageObservationLimits())
		var pgError *pgconn.PgError
		if err == nil || !strings.Contains(err.Error(), "observe columns") || !errors.As(err, &pgError) || pgError.Code != "22023" || result.Complete || tx.committed {
			t.Fatal("query diagnostic lost family/state", err)
		}
	}
	tx := &lineageTestTransaction{oversize: true}
	_, err := ObserveLineage(context.Background(), &lineageTestSource{tx: tx}, DefaultLineageObservationLimits())
	if err == nil || !strings.Contains(err.Error(), "observe functions") {
		t.Fatal("row diagnostic lost family", err)
	}
}

func TestLineageInternalCharIdentityConcatenationUsesExplicitText(t *testing.T) {
	for _, query := range lineageObservationQueries {
		if query.Family == "default_acl" {
			if !strings.Contains(query.SQL, "||d.defaclobjtype::text AS identity") || strings.Contains(query.SQL, "||d.defaclobjtype AS identity") {
				t.Fatal("ambiguous internal-char concat")
			}
		}
		for _, column := range []string{"prokind", "provolatile", "proparallel", "relkind", "relpersistence", "relreplident", "attidentity", "attgenerated", "attstorage", "attcompression", "contype", "polcmd", "ev_enabled", "typtype", "typcategory", "typalign", "typstorage", "deptype"} {
			if strings.Contains(query.SQL, "||p."+column+" AS") || strings.Contains(query.SQL, "||c."+column+" AS") || strings.Contains(query.SQL, "||a."+column+" AS") || strings.Contains(query.SQL, "||t."+column+" AS") {
				t.Fatal("untyped internal-char identity", query.Family, column)
			}
		}
	}
}
