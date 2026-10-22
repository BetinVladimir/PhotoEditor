#!/usr/bin/env python3
"""Project pre-Apply contracts using the same projection as generate-contracts."""
from pathlib import Path
import json
import sys
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'workers'))
from photo_worker.contracts.semantic_preview_projection import (
    PREVIEW_MODELS as MODELS, standalone_schema, project_bundle, rewrite_refs,
)

from photo_worker.contracts.retouch_preview_catalog_projection import project_retouch_preview_catalog


def outputs():
    path=ROOT/'docs/implementation-v2/contracts/schemas.json'
    bundle=project_bundle(json.loads(path.read_text()))
    result={path:json.dumps(bundle,ensure_ascii=False,indent=2)+'\n'}
    for name,model in MODELS:
        target=ROOT/f'packages/contracts/generated/{name}.schema.json'
        result[target]=json.dumps(standalone_schema(name,model),ensure_ascii=False,
                                  indent=2,sort_keys=True)+'\n'
    catalog=ROOT/'docs/implementation-v2/contracts/operations.json'
    result[catalog]=json.dumps(project_retouch_preview_catalog(json.loads(catalog.read_text())),ensure_ascii=False,indent=2)+'\n'
    return result


def main():
    for path,body in outputs().items():
        path.write_text(body)


if __name__=='__main__':main()
