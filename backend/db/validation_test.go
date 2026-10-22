package migrations

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"
)

const validMigration = "-- +goose Up\nSELECT 1;\n-- +goose Down\nDO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;"

func TestMigrationBundlePreservesLegacyUpOnlyHistory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files fstest.MapFS
		valid bool
	}{
		{"valid with historical gap", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte(validMigration)}, "migrations/00003_next.sql": {Data: []byte(validMigration)}}, true},
		{"duplicate versions", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte(validMigration)}, "migrations/00001_second.sql": {Data: []byte(validMigration)}}, false},
		{"invalid filename", fstest.MapFS{"migrations/current.sql": {Data: []byte(validMigration)}}, false},
		{"unused Down is not executed by migrate", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nDROP SCHEMA photo CASCADE;")}}, true},
		{"historical absent Down", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte("-- +goose Up\nSELECT 1;")}}, true},
		{"comment-only Down", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\n-- Forward-only: immutable receipts.")}}, true},
		{"lowercase numbered guard", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nDO $$ BEGIN RAISE EXCEPTION '02491 forward-only'; END $$;")}}, true},
		{"missing Up", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte("SELECT 1;\n-- +goose Down")}}, false},
		{"Down before Up", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte("-- +goose Down\n-- +goose Up\nSELECT 1;")}}, false},
		{"duplicate Up", fstest.MapFS{"migrations/00001_first.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n-- +goose Up")}}, false},
		{"empty", fstest.MapFS{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateBundle(tc.files); (err == nil) != tc.valid {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}

// A hostile filesystem must not bypass the stream budget by reporting a small
// size or implementing fs.ReadFileFS with an unbounded allocation.
type migrationSourceFS struct {
	data              []byte
	reported          int64
	readErr, closeErr error
	consumed          int
	closed            bool
}

func (s *migrationSourceFS) Open(string) (fs.File, error) {
	return &migrationSourceFile{owner: s, reader: bytes.NewReader(s.data)}, nil
}
func (s *migrationSourceFS) ReadFile(string) ([]byte, error) {
	panic("unbounded ReadFile must not be used")
}

type migrationSourceFile struct {
	owner  *migrationSourceFS
	reader *bytes.Reader
}

func (f *migrationSourceFile) Stat() (fs.FileInfo, error) {
	return migrationSourceInfo{size: f.owner.reported}, nil
}
func (f *migrationSourceFile) Read(p []byte) (int, error) {
	if f.owner.readErr != nil {
		return 0, f.owner.readErr
	}
	n, e := f.reader.Read(p)
	f.owner.consumed += n
	return n, e
}
func (f *migrationSourceFile) Close() error { f.owner.closed = true; return f.owner.closeErr }

type migrationSourceInfo struct{ size int64 }

func (i migrationSourceInfo) Name() string       { return "00001_test.sql" }
func (i migrationSourceInfo) Size() int64        { return i.size }
func (i migrationSourceInfo) Mode() fs.FileMode  { return 0o600 }
func (i migrationSourceInfo) ModTime() time.Time { return time.Time{} }
func (i migrationSourceInfo) IsDir() bool        { return false }
func (i migrationSourceInfo) Sys() any           { return nil }

func TestMigrationSourceBoundedRead(t *testing.T) {
	const budget int64 = 4096
	for _, tc := range []struct {
		name              string
		length            int
		reported          int64
		readErr, closeErr error
		valid             bool
		maxRead           int
	}{
		{name: "exact boundary", length: 4096, reported: 4096, valid: true, maxRead: 4096},
		{name: "oversized stat rejected without read", length: 4097, reported: 4097, maxRead: 0},
		{name: "dishonest stat stream refuses extra byte", length: 8192, reported: 1, maxRead: 4097},
		{name: "empty stream despite positive stat", reported: 1, maxRead: 0},
		{name: "zero stat", length: 1, reported: 0, maxRead: 0},
		{name: "negative stat", length: 1, reported: -1, maxRead: 0},
		{name: "read failure", length: 1, reported: 1, readErr: io.ErrUnexpectedEOF, maxRead: 0},
		{name: "close failure", length: 1, reported: 1, closeErr: errors.New("private close detail"), maxRead: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &migrationSourceFS{data: bytes.Repeat([]byte("x"), tc.length), reported: tc.reported, readErr: tc.readErr, closeErr: tc.closeErr}
			raw, err := readMigrationSource(source, "migrations/00001_test.sql", budget)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected admission: %v", err)
			}
			if !source.closed || source.consumed > tc.maxRead {
				t.Fatalf("unclosed or over-budget source: closed=%v read=%d", source.closed, source.consumed)
			}
			if tc.valid && !bytes.Equal(raw, source.data) {
				t.Fatal("source bytes changed")
			}
			if !tc.valid && raw != nil {
				t.Fatal("partial source escaped on failure")
			}
		})
	}
}

func TestMigrationBundleAcceptsReviewedCaptureTransportSize(t *testing.T) {
	// Regression for the exact byte length refused before Goose on actual2616.
	const actualSourceBytes = 27752180
	if maxMigrationSourceBytes != 64<<20 {
		t.Fatal("deployment input budget changed without updating transport contract")
	}
	raw := append([]byte(validMigration), bytes.Repeat([]byte(" "), actualSourceBytes-len(validMigration))...)
	if err := ValidateBundle(fstest.MapFS{"migrations/02616_capture.sql": {Data: raw}}); err != nil {
		t.Fatalf("reviewed transport size refused: %v", err)
	}
}
