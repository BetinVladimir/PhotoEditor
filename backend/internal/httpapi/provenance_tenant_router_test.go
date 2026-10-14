package httpapi

import "photoeditor/backend/internal/provenance"

// Compile-time boundary checks; no HTTP/native runtime acceptance implied.
var _ ExportSigner = (*provenance.TenantSigningRouter)(nil)
var _ SigningPolicyLister = (*provenance.TenantSigningRouter)(nil)
var _ ExportSigner = (*provenance.InstalledSQLSigningService)(nil)
var _ SigningPolicyLister = (*provenance.InstalledSQLSigningService)(nil)
