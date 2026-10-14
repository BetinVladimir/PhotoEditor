import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_read_budget import ImportReadBudget


@pytest.mark.parametrize('maximum', [True, 0, -1, (128 << 20) + 1, 1.5])
def test_import_read_budget_rejects_invalid_installation(maximum):
    with pytest.raises(ValueError):
        ImportReadBudget(maximum)


def test_import_read_budget_counts_repeated_reads_and_binary_separately():
    budget = ImportReadBudget(10)
    budget.consume(4, is_json=True)
    budget.consume(1024, is_json=False)
    budget.consume(6, is_json=True)
    assert budget.remaining_json_bytes == 0
    assert budget.input_bytes == 1034
    with pytest.raises(AdoptionInvalid, match='READ_BUDGET'):
        budget.consume(1, is_json=True)
    assert budget.input_bytes == 1034
    assert budget.remaining_json_bytes == 0


@pytest.mark.parametrize('size,is_json', [(True, True), (-1, True), (1, 1), (1.5, False)])
def test_import_read_budget_invalid_charge_does_not_change_ledger(size, is_json):
    budget = ImportReadBudget(10)
    with pytest.raises(AdoptionInvalid):
        budget.consume(size, is_json=is_json)
    assert budget.input_bytes == 0 and budget.remaining_json_bytes == 10


def test_import_read_budget_bounds_total_input_bytes():
    budget = ImportReadBudget(10)
    budget.consume(128 << 30, is_json=False)
    with pytest.raises(AdoptionInvalid):
        budget.consume(1, is_json=False)
    assert budget.input_bytes == 128 << 30
