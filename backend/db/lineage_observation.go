package migrations

// Read-only semantic catalog observation, never installation authority.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

const LineageObservationFormat = "photo-schema-observation-v1"
const LineageObservationCoverage = "photo-schema-supported-families+extension-routines+all-role-metadata+semantic-goose-ledger-v2"

var ErrUnknownLineage = errors.New("database lineage is absent, incomplete, ambiguous, or unknown")

type LineageObservationLimits struct {
	MaxRows  int
	MaxBytes int
	Timeout  time.Duration
}

func DefaultLineageObservationLimits() LineageObservationLimits {
	return LineageObservationLimits{MaxRows: 100000, MaxBytes: 64 << 20, Timeout: 30 * time.Second}
}
func (l LineageObservationLimits) valid() bool {
	return l.MaxRows > 0 && l.MaxRows <= 100000 && l.MaxBytes > 0 && l.MaxBytes <= 64<<20 && l.Timeout > 0 && l.Timeout <= 30*time.Second
}

type LineageObjectDigest struct {
	Family   string `json:"family"`
	Identity string `json:"identity"`
	SHA256   string `json:"sha256"`
}
type LineageLedgerStep struct {
	Version string `json:"version"`
	Applied bool   `json:"applied"`
}

// Complete covers only the named projection families. It never proves external
// dependency closure, extension binaries, seed data, or installer compatibility.
// Extension-member routines outside photo (including public.digest) are observed;
// non-photo, non-extension dependencies still require independent validation.
type LineageObservation struct {
	Format                               string                `json:"format"`
	Coverage                             string                `json:"coverage"`
	Complete                             bool                  `json:"complete"`
	ExternalDependencyValidationRequired bool                  `json:"external_dependency_validation_required"`
	ServerVersion                        string                `json:"server_version"`
	LedgerSchema                         string                `json:"ledger_schema"`
	Objects                              []LineageObjectDigest `json:"objects"`
	FamilyCounts                         map[string]int        `json:"family_counts"`
	Ledger                               []LineageLedgerStep   `json:"ledger"`
	SemanticSHA256                       string                `json:"semantic_sha256"`
	ExactLedgerSHA256                    string                `json:"exact_ledger_sha256"`
}

// Independent complete snapshot allowlist only. No migration is inferred from a head number.
type LineageSnapshotAuthority struct {
	Identity       string
	SemanticSHA256 string
}
type LineageObservationSource interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func lineageSemanticHash(value LineageObservation) (string, error) {
	value.SemanticSHA256 = ""
	value.ExactLedgerSHA256 = ""
	return canonical.Hash(value)
}
func ClassifyLineageObservation(value LineageObservation, allowed []LineageSnapshotAuthority) (string, error) {
	if value.Format != LineageObservationFormat || value.Coverage != LineageObservationCoverage || !value.Complete || !value.ExternalDependencyValidationRequired || len(value.Objects) == 0 || len(value.Ledger) == 0 || len(allowed) == 0 {
		return "", ErrUnknownLineage
	}
	if len(value.FamilyCounts) != len(lineageObservationQueries) {
		return "", ErrUnknownLineage
	}
	known := map[string]bool{}
	for _, query := range lineageObservationQueries {
		known[query.Family] = true
	}
	counts := map[string]int{}
	last := ""
	for _, object := range value.Objects {
		key := object.Family + "\x00" + object.Identity
		if !known[object.Family] || object.Identity == "" || len(object.Identity) > 16384 || last != "" && key <= last {
			return "", ErrUnknownLineage
		}
		last = key
		if _, err := domain.ParseDigest(object.SHA256); err != nil {
			return "", ErrUnknownLineage
		}
		counts[object.Family]++
	}
	for _, query := range lineageObservationQueries {
		count, present := value.FamilyCounts[query.Family]
		if !present || count < 0 || counts[query.Family] != count {
			return "", ErrUnknownLineage
		}
	}
	if value.LedgerSchema != "public" && value.LedgerSchema != "photo" {
		return "", ErrUnknownLineage
	}
	if len(value.ServerVersion) != 6 || value.ServerVersion[:2] != "18" {
		return "", ErrUnknownLineage
	}
	if _, err := domain.ParseDigest(value.ExactLedgerSHA256); err != nil {
		return "", ErrUnknownLineage
	}
	for _, step := range value.Ledger {
		if len(step.Version) == 0 || len(step.Version) > 19 {
			return "", ErrUnknownLineage
		}
		for _, digit := range step.Version {
			if digit < '0' || digit > '9' {
				return "", ErrUnknownLineage
			}
		}
	}
	if value.FamilyCounts["schema"] != 1 || value.FamilyCounts["ledger_schema"] != 1 || value.FamilyCounts["functions"] == 0 || value.FamilyCounts["relations"] == 0 || value.FamilyCounts["roles"] == 0 {
		return "", ErrUnknownLineage
	}
	observed, err := lineageSemanticHash(value)
	if err != nil || observed != value.SemanticSHA256 {
		return "", ErrUnknownLineage
	}
	ids, hashes := map[string]bool{}, map[string]bool{}
	match := ""
	for _, entry := range allowed {
		if entry.Identity == "" || len(entry.Identity) > 200 || ids[entry.Identity] || hashes[entry.SemanticSHA256] {
			return "", ErrUnknownLineage
		}
		if _, err := domain.ParseDigest(entry.SemanticSHA256); err != nil {
			return "", ErrUnknownLineage
		}
		ids[entry.Identity] = true
		hashes[entry.SemanticSHA256] = true
		if entry.SemanticSHA256 == observed {
			match = entry.Identity
		}
	}
	if match == "" {
		return "", ErrUnknownLineage
	}
	return match, nil
}

func ObserveLineage(ctx context.Context, source LineageObservationSource, limits LineageObservationLimits) (LineageObservation, error) {
	var result LineageObservation
	if source == nil || !limits.valid() {
		return result, ErrUnknownLineage
	}
	bounded, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	if err := bounded.Err(); err != nil {
		return result, err
	}
	transaction, err := source.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = transaction.Rollback(cleanup)
	}()
	for _, statement := range []string{"SET LOCAL search_path=pg_catalog", "SET LOCAL statement_timeout='10000ms'", "SET LOCAL lock_timeout='1000ms'", "SET LOCAL timezone='UTC'", "SET LOCAL datestyle='ISO, YMD'", "SET LOCAL extra_float_digits=3"} {
		if _, err = transaction.Exec(bounded, statement); err != nil {
			return result, err
		}
	}
	if err = transaction.QueryRow(bounded, "SELECT current_setting('server_version_num')").Scan(&result.ServerVersion); err != nil {
		return result, err
	}
	if len(result.ServerVersion) != 6 || result.ServerVersion[:2] != "18" {
		return LineageObservation{}, errors.New("lineage observation requires PostgreSQL 18")
	}
	rows, err := transaction.Query(bounded, lineageUnsupportedSQL)
	if err != nil {
		return LineageObservation{}, err
	}
	for rows.Next() {
		var family string
		var count int64
		if err = rows.Scan(&family, &count); err != nil {
			rows.Close()
			return LineageObservation{}, err
		}
		if count != 0 {
			rows.Close()
			return LineageObservation{}, fmt.Errorf("unsupported photo catalog family: %s", family)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return LineageObservation{}, err
	}
	result.ExternalDependencyValidationRequired = true
	result.Format = LineageObservationFormat
	result.Coverage = LineageObservationCoverage
	result.FamilyCounts = map[string]int{}
	result.Objects = []LineageObjectDigest{}
	totalBytes, totalRows := 0, 0
	for _, query := range lineageObservationQueries {
		result.FamilyCounts[query.Family] = 0
		rows, err = transaction.Query(bounded, query.SQL)
		if err != nil {
			return LineageObservation{}, fmt.Errorf("observe %s query: %w", query.Family, err)
		}
		for rows.Next() {
			var identity, hash string
			if err = rows.Scan(&identity, &hash); err != nil {
				rows.Close()
				return LineageObservation{}, fmt.Errorf("observe %s: %w", query.Family, err)
			}
			totalRows++
			encoded, encodeErr := json.Marshal(LineageObjectDigest{query.Family, identity, hash})
			if encodeErr != nil {
				rows.Close()
				return LineageObservation{}, encodeErr
			}
			totalBytes += len(encoded) + 1
			if totalRows > limits.MaxRows || totalBytes > limits.MaxBytes || len(identity) == 0 || len(identity) > 16384 {
				rows.Close()
				return LineageObservation{}, fmt.Errorf("observe %s: lineage observation bounds exceeded", query.Family)
			}
			if _, err = domain.ParseDigest(hash); err != nil {
				rows.Close()
				return LineageObservation{}, ErrUnknownLineage
			}
			result.Objects = append(result.Objects, LineageObjectDigest{query.Family, identity, hash})
			result.FamilyCounts[query.Family]++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return LineageObservation{}, fmt.Errorf("observe %s: %w", query.Family, err)
		}
	}
	sort.Slice(result.Objects, func(i, j int) bool {
		a, b := result.Objects[i], result.Objects[j]
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		return a.Identity < b.Identity
	})
	for i := 1; i < len(result.Objects); i++ {
		if result.Objects[i].Family == result.Objects[i-1].Family && result.Objects[i].Identity == result.Objects[i-1].Identity {
			return LineageObservation{}, ErrUnknownLineage
		}
	}
	var publicLedger, photoLedger bool
	if err = transaction.QueryRow(bounded, "SELECT to_regclass('public.goose_db_version') IS NOT NULL,to_regclass('photo.goose_db_version') IS NOT NULL").Scan(&publicLedger, &photoLedger); err != nil {
		return LineageObservation{}, err
	}
	if publicLedger == photoLedger {
		return LineageObservation{}, errors.New("missing or ambiguous migration ledger")
	}
	ledgerSQL := lineagePublicLedgerSQL
	result.LedgerSchema = "public"
	if photoLedger {
		ledgerSQL = lineagePhotoLedgerSQL
		result.LedgerSchema = "photo"
	}
	rows, err = transaction.Query(bounded, ledgerSQL)
	if err != nil {
		return LineageObservation{}, err
	}
	exact := []struct {
		ID        string `json:"id"`
		Version   string `json:"version"`
		Applied   bool   `json:"applied"`
		Timestamp string `json:"timestamp"`
	}{}
	for rows.Next() {
		var id, version, stamp string
		var applied bool
		if err = rows.Scan(&id, &version, &applied, &stamp); err != nil {
			rows.Close()
			return LineageObservation{}, err
		}
		totalRows++
		totalBytes += len(id) + len(version) + len(stamp) + 128
		if totalRows > limits.MaxRows || totalBytes > limits.MaxBytes {
			rows.Close()
			return LineageObservation{}, errors.New("lineage ledger bounds exceeded")
		}
		result.Ledger = append(result.Ledger, LineageLedgerStep{version, applied})
		exact = append(exact, struct {
			ID        string `json:"id"`
			Version   string `json:"version"`
			Applied   bool   `json:"applied"`
			Timestamp string `json:"timestamp"`
		}{id, version, applied, stamp})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return LineageObservation{}, err
	}
	if result.FamilyCounts["schema"] != 1 || result.FamilyCounts["ledger_schema"] != 1 || result.FamilyCounts["functions"] == 0 || result.FamilyCounts["relations"] == 0 || len(result.Ledger) == 0 {
		return LineageObservation{}, ErrUnknownLineage
	}
	result.Complete = true
	if result.ExactLedgerSHA256, err = canonical.Hash(exact); err != nil {
		return LineageObservation{}, err
	}
	if result.SemanticSHA256, err = lineageSemanticHash(result); err != nil {
		return LineageObservation{}, err
	}
	output, encodeErr := canonical.Marshal(result)
	if encodeErr != nil || len(output) > limits.MaxBytes {
		return LineageObservation{}, errors.New("lineage observation output bounds exceeded")
	}
	if err = bounded.Err(); err != nil {
		return LineageObservation{}, err
	}
	if err = transaction.Commit(bounded); err != nil {
		return LineageObservation{}, err
	}
	return result, nil
}
