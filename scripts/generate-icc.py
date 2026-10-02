#!/usr/bin/env python3
"""Generate deterministic project-owned ICC profiles using installed LittleCMS2."""
from pathlib import Path
import argparse
import hashlib
import json
import sys
ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'workers'))
from photo_worker.color.icc import LittleCMS
parser = argparse.ArgumentParser()
parser.add_argument('--check', action='store_true')
args = parser.parse_args()
engine = LittleCMS()
profiles = []
for name in ['work_rec2020_gamma24_v1', 'srgb_v1', 'linear_srgb_v1']:
    data = engine.generate(name)
    path = ROOT/'resources/icc'/f'{name}.icc'
    if args.check:
        if not path.exists() or path.read_bytes() != data:
            raise SystemExit(f'ICC drift: {name}; generator LCMS={engine.version}')
    else:
        path.write_bytes(data)
    profiles.append({'id':name,'path':path.name,'sha256':hashlib.sha256(data).hexdigest(),
                     'bytes':len(data),'license':'CC0-1.0','generator_lcms_version':engine.version})
manifest = json.dumps({'format_version':1,'profiles':profiles}, indent=2)+'\n'
path = ROOT/'resources/icc/profiles.lock.json'
if args.check:
    if path.read_text() != manifest:
        raise SystemExit('ICC manifest drift')
else:
    path.write_text(manifest)
print(f'ICC profiles verified; LCMS={engine.version}')
