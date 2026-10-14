package interchange

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/semanticproposal"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type ExportStatsPersonPolicyFactory interface {
	BuildExportStatsPersonPolicy(context.Context, CreateIntent, FrozenExportDocuments, ExportDocumentCatalog, ImportDocumentObjects) (ExportStatsPersonPolicyAuthority, error)
}

// Expected and Report are selected by deployment operators, never the archive.
// The request's immutable snapshot supplies the complete source binding set.
type SQLExportStatsPersonPolicyInstallation struct {
	Expected v2qualification.Expected
	Report   v2input.Ref
}

func (s SQLExportStatsPersonPolicyInstallation) BuildExportStatsPersonPolicy(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, catalog ExportDocumentCatalog, objects ImportDocumentObjects) (ExportStatsPersonPolicyAuthority, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scoped, err := exportPolicyCatalogConfig(catalog)
	if err != nil || scoped.DB == nil || objects == nil {
		return nil, v2input.ErrInvalid
	}
	actual, err := FreezeCreateIntent(scoped.Tenant, scoped.Who, scoped.Request, scoped.Key)
	if err != nil {
		return nil, err
	}
	if actual.RequestSHA != intent.RequestSHA || actual.Identity != intent.Identity {
		return nil, v2input.ErrInvalid
	}
	checked, _, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return nil, err
	}
	if string(scoped.Frozen.Body()) != string(checked.Body()) {
		return nil, v2input.ErrInvalid
	}
	var payload exportFreezePayload
	if err := json.Unmarshal(checked.Body(), &payload); err != nil {
		return nil, err
	}
	var snapshot v2input.SessionInputSnapshot
	if err := decodeImportDocument([]byte(payload.SnapshotJCS), &snapshot); err != nil {
		return nil, err
	}
	bindings := make([]v2input.SessionVersionBinding, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		bindings = append(bindings, item.Binding)
	}
	reader := semanticproposal.SQLTransferSourceReader{DB: scoped.DB, Objects: objects, Bucket: scoped.Bucket, ProjectID: scoped.Request.ProjectID}
	if err := validateExportMaskAuthorityInstallation(ctx, scoped.DB); err != nil {
		return nil, err
	}
	return NewExportStatsPersonPolicy(reader, scoped.Who, scoped.Tenant, bindings, s.Report, s.Expected)
}
func exportPolicyCatalogConfig(catalog ExportDocumentCatalog) (SQLExportDocumentCatalog, error) {
	switch scoped := catalog.(type) {
	case SQLExportDocumentCatalog:
		return scoped, nil
	case SQLExportResultInputCatalog:
		source := scoped.Export
		return SQLExportDocumentCatalog{DB: source.DB, Tenant: source.Tenant, Who: source.Who, Request: source.Request, Key: source.Key, Frozen: source.Frozen, Bucket: source.Bucket}, nil
	default:
		return SQLExportDocumentCatalog{}, v2input.ErrInvalid
	}
}

var _ ExportStatsPersonPolicyFactory = SQLExportStatsPersonPolicyInstallation{}
