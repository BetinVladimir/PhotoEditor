import json
import os
from pathlib import Path
import subprocess

from photo_worker.contracts.base import canonical_bytes
from test_region_calibration import calibrated


def test_go_python_region_workload_catalog_and_full_budget_parity():
    cases=[]
    for alpha in (False,True):
        raw,profile,params,source=calibrated(alpha)
        cases.append(dict(Catalog=json.loads(raw),Profile=profile,
            Params=params.model_dump(mode='json'),Source=source.model_dump(mode='json')))
    env=dict(os.environ,PHOTO_REGION_CALIBRATION_CASES=canonical_bytes(cases).decode(),
        GOCACHE='/private/tmp/photoeditor-go-cache')
    result=subprocess.run(['go','test','./internal/v2region','-run','^TestRegionCalibrationPythonWireParity$','-count=1'],
        cwd=Path(__file__).resolve().parents[2]/'backend',env=env,capture_output=True,text=True,timeout=45)
    assert result.returncode==0,result.stdout+result.stderr
