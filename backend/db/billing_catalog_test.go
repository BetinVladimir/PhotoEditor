package migrations

import (
	"io/fs"
	"testing"
)

func TestBillingCatalogIncludedInValidEmbeddedBundle(t *testing.T) {
	for _, name := range []string{
		"migrations/02598_billing_catalog_scaffold.sql",
		"migrations/02599_billing_catalog_seed.sql",
		"migrations/02600_billing_quota_accounting.sql",
		"migrations/02601_billing_quota_functions.sql",
		"migrations/02602_billing_apply_bindings.sql",
		"migrations/02603_billing_apply_workflow.sql",
		"migrations/02604_billing_routed_apply.sql",
		"migrations/02605_billing_render_workflow.sql",
		"migrations/02606_billing_editor_workflow.sql",
		"migrations/02607_billing_operations_workflow.sql",
		"migrations/02608_billing_paid_operation_funding.sql",
		"migrations/02609_billing_selected_workflow.sql",
	} {
		if _, err := fs.ReadFile(FS, name); err != nil {
			t.Fatalf("billing migration missing from deployed bundle: %s: %v", name, err)
		}
	}
	if err := ValidateBundle(FS); err != nil {
		t.Fatalf("embedded migration bundle invalid: %v", err)
	}
}
