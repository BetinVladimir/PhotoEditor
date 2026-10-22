package main

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/semanticreview"
	"testing"
)

func TestPreviewInstallationEvidenceIsTenantAndOperationBound(t *testing.T) {
	tenant, other := uuid.New(), uuid.New()
	service := &tenantPreviewAdmission{backgroundCleanup: map[uuid.UUID]*semanticreview.BackgroundCleanupPreviewAdmissionService{tenant: {}}, resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, semanticreview.PreviewRequest) (string, error) {
		return "background_cleanup", nil
	}}
	if !service.SemanticOperationInstalled(tenant, "semantic.preview_background_cleanup") {
		t.Fatal("installed own preview missing")
	}
	for _, entry := range []struct {
		tenant    uuid.UUID
		operation string
	}{{other, "semantic.preview_background_cleanup"}, {tenant, "semantic.preview_remove_object"}, {tenant, "semantic.adopt_shown_background_cleanup"}} {
		if service.SemanticOperationInstalled(entry.tenant, entry.operation) {
			t.Fatal("borrowed installation evidence")
		}
	}
	service.resolve = nil
	if service.SemanticOperationInstalled(tenant, "semantic.preview_background_cleanup") {
		t.Fatal("missing resolver advertised ready")
	}
	var missing *tenantPreviewAdmission
	if missing.SemanticOperationInstalled(tenant, "semantic.preview_background_cleanup") {
		t.Fatal("nil service advertised ready")
	}
}

func TestRetouchPreviewInstallationEvidenceIsTenantAndOperationBound(t *testing.T) {
	tenant, other := uuid.New(), uuid.New()
	service := &tenantPreviewAdmission{retouch: map[uuid.UUID]*semanticreview.RetouchPreviewAdmissionService{tenant: {}}, resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, semanticreview.PreviewRequest) (string, error) {
		return "retouch_region", nil
	}}
	if !service.SemanticOperationInstalled(tenant, "semantic.preview_retouch_region") {
		t.Fatal("installed own preview missing")
	}
	for _, entry := range []struct {
		tenant    uuid.UUID
		operation string
	}{{other, "semantic.preview_retouch_region"}, {tenant, "semantic.preview_remove_object"}, {tenant, "semantic.adopt_shown_retouch_region"}} {
		if service.SemanticOperationInstalled(entry.tenant, entry.operation) {
			t.Fatal("borrowed installation evidence")
		}
	}
	service.resolve = nil
	if service.SemanticOperationInstalled(tenant, "semantic.preview_retouch_region") {
		t.Fatal("missing resolver advertised ready")
	}
	var missing *tenantPreviewAdmission
	if missing.SemanticOperationInstalled(tenant, "semantic.preview_retouch_region") {
		t.Fatal("nil service advertised ready")
	}
}

func TestRemovalPreviewInstallationEvidenceIsTenantAndOperationBound(t *testing.T) {
	tenant, other := uuid.New(), uuid.New()
	service := &tenantPreviewAdmission{removal: map[uuid.UUID]*semanticreview.RemovalPreviewAdmissionService{tenant: {}}, resolve: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, semanticreview.PreviewRequest) (string, error) {
		return "remove_object", nil
	}}
	if !service.SemanticOperationInstalled(tenant, "semantic.preview_remove_object") {
		t.Fatal("installed own preview missing")
	}
	for _, entry := range []struct {
		tenant    uuid.UUID
		operation string
	}{{other, "semantic.preview_remove_object"}, {tenant, "semantic.preview_background_cleanup"}, {tenant, "semantic.adopt_shown_removal"}} {
		if service.SemanticOperationInstalled(entry.tenant, entry.operation) {
			t.Fatal("borrowed installation evidence")
		}
	}
	service.resolve = nil
	if service.SemanticOperationInstalled(tenant, "semantic.preview_remove_object") {
		t.Fatal("missing resolver advertised ready")
	}
	var missing *tenantPreviewAdmission
	if missing.SemanticOperationInstalled(tenant, "semantic.preview_remove_object") {
		t.Fatal("nil service advertised ready")
	}
}
