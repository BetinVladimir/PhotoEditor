package workflow

import (
    "context"
    "strings"
    "testing"
    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
)

type archiveContextRows struct { pgx.Rows; items []downloadArchiveRow; index int; closed bool; cancel context.CancelFunc }
func (r *archiveContextRows) Next() bool { if r.index >= len(r.items) { if r.cancel != nil { r.cancel() }; return false }; r.index++; return true }
func (r *archiveContextRows) Close() { r.closed = true }
func (r *archiveContextRows) Err() error { return nil }
func (r *archiveContextRows) Scan(v ...any) error {
    item := r.items[r.index-1]
    *(v[0].(*uuid.UUID)) = item.snapshot; *(v[1].(*string)) = item.snapshotDigest
    *(v[2].(*int)) = item.volume; *(v[3].(*string)) = item.kind; *(v[4].(*int64)) = item.ordinal
    *(v[5].(*uuid.UUID)) = item.artifact; *(v[6].(*string)) = item.sha; *(v[7].(*int64)) = item.bytes
    *(v[8].(*string)) = item.name; *(v[9].(*uuid.UUID)) = item.producer
    return nil
}
type archiveContextDB struct { rows *archiveContextRows }
func (d archiveContextDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return d.rows, nil }

func TestArchiveContextOwnsBoundedOrderedInventoryAndClosesRows(t *testing.T) {
    snapshot := uuid.New()
    row := downloadArchiveRow{snapshot: snapshot, snapshotDigest: strings.Repeat("a", 64), volume: 1, kind: "zip",
        artifact: uuid.New(), sha: strings.Repeat("b", 64), bytes: 1, name: "frame.jpg", producer: uuid.New()}
    for _, kind := range []string{"ordered", "invalid_single", "ordinal_bound", "cancelled_eof"} {
        t.Run(kind, func(t *testing.T) {
            first, second := row, row
            first.ordinal = 2; first.name = "second.jpg"
            second.ordinal = 0; second.name = "first.jpg"
            rows := &archiveContextRows{items: []downloadArchiveRow{first, second}}
            ctx, cancel := context.WithCancel(context.Background())
            defer cancel()
            if kind == "invalid_single" { rows.items[0].kind = "single"; rows.items[0].sha = "invalid" }
            if kind == "ordinal_bound" { rows.items[0].ordinal = MaxDownloadItems }
            if kind == "cancelled_eof" { rows.cancel = cancel }
            spec, err := (DownloadArchiveContextSource{DB: archiveContextDB{rows: rows}}).Load(ctx, uuid.New(), uuid.New())
            if !rows.closed { t.Fatal("rows leaked") }
            if kind == "ordered" {
                if err != nil || len(spec.Volumes) != 1 || spec.Volumes[0].Entries[0].Ordinal != 0 || spec.Volumes[0].Entries[1].Ordinal != 2 { t.Fatal("unordered context escaped", err) }
            } else {
                if err == nil || spec.RunID != uuid.Nil { t.Fatal("invalid context escaped") }
                if kind == "cancelled_eof" && err != context.Canceled { t.Fatal("cancellation lost", err) }
            }
        })
    }
}
