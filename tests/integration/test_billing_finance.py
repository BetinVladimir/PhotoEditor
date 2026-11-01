"""Finance group integration tests (FIN-00 scaffold; tests added by FIN-T* and FIN-F* tasks).

Applies finance migrations (02660, 02661) to an isolated DB and verifies
table constraints, FK, RLS isolation, and function invariants.
No secrets in fixtures (T8). Working DB never touched (R30).
"""
from pathlib import Path
import pytest

ROOT = Path(__file__).resolve().parents[2]
FINANCE_MIGRATIONS = [
    '02660_billing_finance_schema.sql',
    '02661_billing_finance_functions.sql',
]
pytestmark = pytest.mark.migration_head(17)


# ---------------------------------------------------------------------------
# Scaffold placeholder — passes without DB, exercises import only
# ---------------------------------------------------------------------------

def test_finance_scaffold_import():
    """FIN-00: scaffold exists and imports correctly."""
    assert ROOT.joinpath('docs/billing/contracts/finance-tables.json').exists()
    assert ROOT.joinpath('docs/billing/contracts/finance-functions.json').exists()
    assert ROOT.joinpath('scripts/generate-billing-finance.py').exists()
