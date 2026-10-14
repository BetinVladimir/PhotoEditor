"""Run the lens role/context guard against an owned fully migrated database."""
from pathlib import Path
import runpy


def test_lens_scheduler_boundary_on_clean_migrations(db):
    root=Path(__file__).resolve().parents[2]
    checks=runpy.run_path(str(root/'tests/platform/test_v25_lens_adoption_migration.py'))
    checks['check_lens_adoption_boundary'](db)
