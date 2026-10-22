"""Static migration contract; runtime SQL is reserved for the test phase."""
from pathlib import Path
import unittest
ROOT = Path(__file__).resolve().parents[2]

class ImportMaskGrantVariableFenceTests(unittest.TestCase):
    def test_exact_candidate_variable(self):
        sql = (ROOT / 'backend/db/migrations/02168_v30_import_mask_grant_variable_fence.sql').read_text()
        for fragment in ('CREATE OR REPLACE FUNCTION photo.grant_v30_import_person_mask(', 'v_mask_id uuid;', 'c.mask_id=v_mask_id', 'AND mask_id=v_mask_id', 'p_override_id,override_sha,v_mask_id,mask_sha,source_version', 'SECURITY DEFINER SET search_path=pg_catalog,photo'):
            self.assertIn(fragment, sql)
        for fragment in ('mask_id=mask_id', 'goose Down', 'GRANT '):
            self.assertNotIn(fragment, sql)
