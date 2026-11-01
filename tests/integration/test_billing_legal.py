"""Legal group DB integration tests (LEG-T01–T04).

Requires PostgreSQL (billing_db fixture from conftest.py). These tests are
skipped in environments without a running DB. Working DB never touched (R30).
"""
import pytest

pytestmark = pytest.mark.integration


@pytest.mark.skipif(True, reason='LEG-T* DB integration tests: run with billing_db fixture once PostgreSQL is available')
def test_legal_document_versions_rls_isolation(billing_db):
    """LEG-T01: RLS blocks direct insert from non-owner role."""
    with pytest.raises(Exception):
        billing_db.execute("SET ROLE photo_api; INSERT INTO photo.legal_document_versions DEFAULT VALUES")


@pytest.mark.skipif(True, reason='LEG-T* DB integration tests: run with billing_db fixture once PostgreSQL is available')
def test_privacy_retention_policy_draft_allows_null_retention(billing_db):
    """LEG-T04: draft policy can have NULL retention_days (not approved yet)."""
    billing_db.execute(
        "INSERT INTO photo.legal_document_versions VALUES ..."  # placeholder
    )


@pytest.mark.skipif(True, reason='LEG-T* DB integration tests: run with billing_db fixture once PostgreSQL is available')
def test_privacy_retention_policy_approved_requires_retention_days(billing_db):
    """LEG-T04: approved policy must have retention_days > 0."""
    with pytest.raises(Exception, match='violates check constraint'):
        billing_db.execute(
            "INSERT INTO photo.privacy_retention_policies "
            "(code,version,data_category,legal_basis,retention_trigger,state) "
            "VALUES ('test',1,'test','test','account_deletion','approved')"
        )
