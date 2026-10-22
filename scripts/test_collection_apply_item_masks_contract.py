"""Additive item-bound Apply API, with historical worker ABI retained."""
from pathlib import Path
from copy import deepcopy
from hashlib import sha256
from uuid import uuid4
import json,unittest
from jsonschema import Draft202012Validator,FormatChecker
ROOT=Path(__file__).resolve().parents[1]
class ItemMaskContract(unittest.TestCase):
    def setUp(self):
        self.bundle=json.loads((ROOT/'docs/implementation-v2/contracts/schemas.json').read_text())
        self.validator=Draft202012Validator({'$ref':'#/$defs/CollectionApplyRequest','$defs':self.bundle['$defs']},format_checker=FormatChecker())
        self.request={'source_revision':1,'style_version_id':str(uuid4()),'scope':'selected','item_ids':[str(uuid4()),str(uuid4())]}
        self.request['item_mask_overrides']=[{'item_id':item,'overrides_id':str(uuid4())} for item in self.request['item_ids']]
    def test_complete_two_source_request_and_legacy_without_map(self):
        self.validator.validate(self.request)
        legacy=deepcopy(self.request);del legacy['item_mask_overrides'];self.validator.validate(legacy)
        legacy['overrides_id']=str(uuid4());self.validator.validate(legacy)
    def test_map_cannot_have_extra_body_or_zero_identity(self):
        for field,value in [('body',{}),('item_id','00000000-0000-0000-0000-000000000000'),('overrides_id','not-a-uuid')]:
            candidate=deepcopy(self.request);candidate['item_mask_overrides'][0][field]=value
            with self.subTest(field=field):self.assertTrue(list(self.validator.iter_errors(candidate)))
    def test_nonempty_per_item_map_cannot_mix_global_override(self):
        candidate=deepcopy(self.request);candidate['overrides_id']=str(uuid4())
        self.assertTrue(list(self.validator.iter_errors(candidate)))
        candidate['overrides_id']=None;self.validator.validate(candidate)
        candidate['item_mask_overrides']=[];candidate['overrides_id']=str(uuid4());self.validator.validate(candidate)
    def test_build_pin_matches_exact_bundle_old_generation_remains_explicit(self):
        import sys
        sys.path.insert(0,str(ROOT/'workers'))
        from photo_worker.v2.contract_lock import SCHEMAS_SHA256,COPY_FENCE_SCHEMAS_SHA256
        self.assertEqual(SCHEMAS_SHA256,sha256((ROOT/'docs/implementation-v2/contracts/schemas.json').read_bytes()).hexdigest())
        self.assertEqual(COPY_FENCE_SCHEMAS_SHA256,'75e3bcf5679402efe777b409f919bdcd594f92be5a163975ccf66e27dc3b6ee8')
        self.assertNotEqual(SCHEMAS_SHA256,COPY_FENCE_SCHEMAS_SHA256)
