package privacy

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPreferencesUpdateValidate(t *testing.T) {
	yes := true
	if err := (PreferencesUpdate{Analytics: &yes}).Validate(); err != nil {
		t.Fatalf("analytics-only update must be valid: %v", err)
	}
	if err := (PreferencesUpdate{Marketing: &yes}).Validate(); err != nil {
		t.Fatalf("marketing-only update must be valid: %v", err)
	}
	if err := (PreferencesUpdate{Analytics: &yes, Marketing: &yes}).Validate(); err != nil {
		t.Fatalf("both fields must be valid: %v", err)
	}
	if err := (PreferencesUpdate{}).Validate(); err == nil {
		t.Fatal("empty update must return error")
	}
}

func TestDeletionRequestValidate_valid(t *testing.T) {
	req := DeletionRequest{
		PrivacyCaseID:   uuid.New(),
		RecentAuthGrant: "grant_token",
		OwnershipResolution: []OwnershipResolution{
			{TenantID: uuid.New(), Action: "close"},
			{TenantID: uuid.New(), Action: "transfer"},
		},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("valid deletion request rejected: %v", err)
	}
}

func TestDeletionRequestValidate_missingPrivacyCaseID(t *testing.T) {
	req := DeletionRequest{RecentAuthGrant: "grant"}
	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "privacy_case_id") {
		t.Fatalf("missing privacy_case_id must return error, got %v", err)
	}
}

func TestDeletionRequestValidate_missingRecentAuth(t *testing.T) {
	req := DeletionRequest{PrivacyCaseID: uuid.New()}
	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "recent_auth_grant") {
		t.Fatalf("missing recent_auth_grant must return error, got %v", err)
	}
}

func TestDeletionRequestValidate_missingTenantID(t *testing.T) {
	req := DeletionRequest{
		PrivacyCaseID:   uuid.New(),
		RecentAuthGrant: "grant",
		OwnershipResolution: []OwnershipResolution{
			{TenantID: uuid.Nil, Action: "close"},
		},
	}
	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "tenant_id") {
		t.Fatalf("nil tenant_id must return error, got %v", err)
	}
}

func TestDeletionRequestValidate_invalidAction(t *testing.T) {
	req := DeletionRequest{
		PrivacyCaseID:   uuid.New(),
		RecentAuthGrant: "grant",
		OwnershipResolution: []OwnershipResolution{
			{TenantID: uuid.New(), Action: "invalid"},
		},
	}
	if err := req.Validate(); err == nil || !strings.Contains(err.Error(), "action") {
		t.Fatalf("invalid action must return error, got %v", err)
	}
}

func TestFieldErrorMessage(t *testing.T) {
	err := errMissingField("my_field")
	if !strings.Contains(err.Error(), "my_field") {
		t.Fatalf("error message must contain field name, got %q", err.Error())
	}
	err2 := errInvalidField("my_field")
	if !strings.Contains(err2.Error(), "my_field") {
		t.Fatalf("error message must contain field name, got %q", err2.Error())
	}
}
