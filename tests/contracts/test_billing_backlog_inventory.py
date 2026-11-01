"""The remaining-work inventory must survive completed tasks and repacking."""
import importlib.util
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
TOOLS = ROOT / 'docs/billing/tools'


def load_coverage():
    spec = importlib.util.spec_from_file_location('billing_coverage_test', TOOLS / 'check-covered.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_target_inventory_includes_legal_and_both_outbox_functions(tmp_path):
    module = load_coverage()
    module.DOC_04 = tmp_path / 'targets.md'
    module.DOC_04.write_text('| account_claim_deletion_step | batch |\n'
                             '| billing_claim_outbox / billing_finish_outbox | messages |\n'
                             '| billing_claim_outbox | repeated |\n')
    assert module.load_target_04() == [
        'account_claim_deletion_step', 'billing_claim_outbox', 'billing_finish_outbox']


def test_backlog_coverage_includes_account_functions(tmp_path):
    module = load_coverage()
    module.BACKLOG_DIR = tmp_path
    (tmp_path / 'BACKLOG-legal-1.md').write_text(
        '### LEG-F02 — функция `account_claim_deletion_step`\n')
    assert module.load_backlog_functions() == {'account_claim_deletion_step': 'LEG-F02'}


def fixture_repository(tmp_path):
    shutil.copytree(ROOT / 'docs/billing', tmp_path / 'docs/billing',
                    ignore=shutil.ignore_patterns('__pycache__'))
    for kind in ('contracts', 'integration'):
        target = tmp_path / 'tests' / kind
        target.mkdir(parents=True)
        for path in (ROOT / 'tests' / kind).glob('test_billing*.py'):
            shutil.copyfile(path, target / path.name)
    migrations = tmp_path / 'backend/db/migrations'
    migrations.mkdir(parents=True)
    return migrations


def generate(root, check=False):
    return subprocess.run([sys.executable, str(root / 'docs/billing/tools/gen-backlog.py'),
                           *(['--check'] if check else [])], capture_output=True, text=True)


def function_task(root, name):
    hits = []
    for path in (root / 'docs/billing/backlog').glob('BACKLOG-*.md'):
        hits += re.findall(r'^### ([A-Z]+-F\d+) — функция `' + name + '`$',
                           path.read_text(), re.M)
    return hits


def test_completed_function_does_not_renumber_remaining_tasks(tmp_path):
    migrations = fixture_repository(tmp_path)
    assert generate(tmp_path).returncode == 0
    before = function_task(tmp_path, 'account_claim_deletion_step')
    assert len(before) == 1
    (migrations / '02668_test.sql').write_text('CREATE FUNCTION photo.account_accept_legal() RETURNS void;\n')
    assert generate(tmp_path).returncode == 0
    assert function_task(tmp_path, 'account_accept_legal') == []
    assert function_task(tmp_path, 'account_claim_deletion_step') == before


def test_repacking_removes_stale_generated_chunks_and_preserves_notes(tmp_path):
    fixture_repository(tmp_path)
    assert generate(tmp_path).returncode == 0
    backlog = tmp_path / 'docs/billing/backlog'
    stale = backlog / 'BACKLOG-legal-999.md'
    stale.write_text('obsolete generated task')
    notes = backlog / 'owner-notes.md'
    notes.write_text('keep this note')
    assert generate(tmp_path, check=True).returncode == 1
    assert generate(tmp_path).returncode == 0
    assert not stale.exists()
    assert notes.read_text() == 'keep this note'
    assert generate(tmp_path, check=True).returncode == 0


def test_alter_reference_is_not_implementation_and_compound_rows_are_split(tmp_path):
    migrations = fixture_repository(tmp_path)
    (migrations / '02668_test.sql').write_text('ALTER FUNCTION photo.billing_claim_outbox() OWNER TO photo_fn_owner;\n')
    assert generate(tmp_path).returncode == 0
    assert len(function_task(tmp_path, 'billing_claim_outbox')) == 1
    assert len(function_task(tmp_path, 'billing_finish_outbox')) == 1
    headings = []
    for path in (tmp_path / 'docs/billing/backlog').glob('BACKLOG-*.md'):
        headings.extend(re.findall(r'^### ([A-Z][A-Z0-9-]*) —', path.read_text(), re.M))
    assert len(headings) == len(set(headings))
