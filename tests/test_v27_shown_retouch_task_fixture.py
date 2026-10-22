"""Minimal own shown-retouch input, no receipt/qualification authority."""
from uuid import UUID,uuid5
from test_v27_preview_adoption_contract import fixture as exposure_fixture


def fixture():
    value=exposure_fixture()
    value['preview_task_id']=str(uuid5(UUID(str(value['preview_run_id'])),
        'semantic.preview_retouch_region:'+str(value['target_id'])))
    # This fixture represents the JSON Params document, not Python-native UUID input.
    import json
    return json.loads(json.dumps(value, default=str))
