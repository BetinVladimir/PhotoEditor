package provenance

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type signingInputCursor struct {
	entries []v2input.Entry
	index   int
}

func (c *signingInputCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Entry{}, false, err
	}
	if c.index >= len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	entry := c.entries[c.index]
	c.index++
	return entry, true, nil
}

type SigningInputTransport struct {
	tenant, run, task uuid.UUID
	entries           []v2input.Entry
	built             v2input.Built
}

func (t SigningInputTransport) Built() v2input.Built {
	b := t.built
	b.Chunks = append([]v2input.Ref(nil), b.Chunks...)
	return b
}
func (t SigningInputTransport) Entries() []v2input.Entry {
	return append([]v2input.Entry(nil), t.entries...)
}

// PublishSigningInputs follows committed blocked materialization. Documents
// must authenticate scheduler task/run reservation; no registration or graph
// sealing occurs during physical publication.
func PublishSigningInputs(ctx context.Context, who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string, documents v2input.DocumentPublisher) (SigningInputTransport, error) {
	var zero SigningInputTransport
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if documents == nil {
		return zero, ErrInvalid
	}
	if _, err := compileSigningPending(who, ready, prepared, bucket); err != nil {
		return zero, err
	}
	entries := prepared.compiled.Inputs()
	task := prepared.compiled.Task()
	params, _ := prepared.compiled.ParamsDocument()
	if len(entries) != 3 {
		return zero, ErrInvalid
	}
	digest, err := canonical.Hash(entries)
	if err != nil || digest != task.InputDigest {
		return zero, ErrInvalid
	}
	i := prepared.intent
	built, err := v2input.BuildInputClosure(ctx, i.task, &signingInputCursor{entries: entries}, v2input.ScopedPublisher{Documents: documents, Tenant: i.tenant, Run: i.run})
	if err != nil {
		return zero, err
	}
	if built.Count != 3 || built.SHA256 != digest || len(built.Chunks) != 1 || !ref(built.Manifest) {
		return zero, ErrInvalid
	}
	seen := map[uuid.UUID]bool{params.ArtifactID: true}
	for _, entry := range entries {
		if seen[entry.Ref.ArtifactID] {
			return zero, ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
	}
	for _, document := range append([]v2input.Ref{built.Manifest}, built.Chunks...) {
		if !ref(document) || seen[document.ArtifactID] {
			return zero, ErrInvalid
		}
		seen[document.ArtifactID] = true
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	built.Chunks = append([]v2input.Ref(nil), built.Chunks...)
	return SigningInputTransport{tenant: i.tenant, run: i.run, task: i.task, entries: entries, built: built}, nil
}

// Register checks current saved signer scope and writes/seals task inputs in a
// scheduler transaction. Caller must roll back on error; it does not activate
// the run or establish native SDK qualification. All object I/O precedes this.
func (t SigningInputTransport) Register(ctx context.Context, tx pgx.Tx, who auth.Identity, ready ReadySigningExport, prepared PreparedSigningActivation, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return ErrInvalid
	}
	if _, err := compileSigningPending(who, ready, prepared, bucket); err != nil {
		return err
	}
	i := prepared.intent
	if t.tenant != i.tenant || t.run != i.run || t.task != i.task || t.built.Count != 3 || len(t.built.Chunks) != 1 {
		return ErrInvalid
	}
	expected, err := canonical.Marshal(prepared.compiled.Inputs())
	if err != nil {
		return err
	}
	actual, err := canonical.Marshal(t.entries)
	if err != nil || !bytes.Equal(expected, actual) || bodyDigest(actual) != t.built.SHA256 || t.built.SHA256 != prepared.compiled.Task().InputDigest {
		return ErrInvalid
	}
	task, err := canonical.Marshal(prepared.compiled.Task())
	if err != nil {
		return err
	}
	var accepted bool
	if err := tx.QueryRow(ctx, `SELECT photo.v31_signing_transport_ready($1,$2,$3,$4,$5,$6::jsonb)`, i.tenant, who.UserID, who.SessionID, i.key, i.requestSHA, string(task)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return artifactjson.ErrStale
	}
	params, _ := prepared.compiled.ParamsDocument()
	direct := []v2input.Ref{params}
	for _, entry := range t.entries {
		direct = append(direct, entry.Ref)
	}
	open := func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &signingInputCursor{entries: t.Entries()}, nil
	}
	return v2input.RegisterInTransaction(ctx, tx, i.tenant, i.run, i.task, t.Built(), open, direct)
}
