#!/usr/bin/env python3
"""Validate real e2e artifacts without declaring visual studio acceptance."""
import json
from pathlib import Path
import sys
from PIL import Image
import tifffile
from photoeditor.io import digest

root=Path(__file__).resolve().parents[1]
out=root/'e2e/output'
latest=json.loads((out/'latest.json').read_text())
run=out/'runs'/latest['run_id']
report=json.loads((run/'report.json').read_text())
assert report['state']=='completed'
assert len(report['images'])==6 and report['failed']==0
faces=0
for item in report['images']:
    assert digest(root/'e2e/input'/item['relative'])==item['input_sha256']
    for rel,sha in item['outputs'].items():assert digest(run/rel)==sha
    faces+=item['metrics']['faces_detected']
    assert not any('face_count_mismatch' in s for s in item['issues'])
assert faces==10
masters=list((run/'images').glob('*.tif'));deliveries=list((run/'delivery').glob('*.jpg'))
assert len(masters)==len(deliveries)==6
for path in masters:
    with tifffile.TiffFile(path) as tf:
        p=tf.pages[0]
        assert p.shape==(4000,6000,3) and p.bitspersample==16
        assert 34675 in p.tags
for path in deliveries:
    with Image.open(path) as im:
        assert im.size==(6000,4000) and im.format=='JPEG' and im.info.get('icc_profile')
        im.verify()
assert len(list((run/'review/faces').glob('*.jpg')))==20
print(json.dumps({'technical_e2e':'PASS','photos':6,'faces':faces,'run_dir':str(run),
                  'visual_acceptance':json.loads((run/'acceptance.json').read_text())['visual_status']},indent=2))
