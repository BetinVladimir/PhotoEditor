"""Actual image-processor Python/native bytes used by installed model adapters."""
from importlib.util import find_spec
from pathlib import Path


def processing_component_paths():
    result={};total=0
    roots=[]
    for name in ('transformers','PIL'):
        spec=find_spec(name)
        if spec is None or spec.origin is None:raise ValueError('PROCESSING_PACKAGE_UNAVAILABLE')
        root=Path(spec.origin).resolve(strict=True).parent
        roots.append((name,root))
        if name=='PIL':
            vendor=root.parent/'pillow.libs'
            if vendor.is_dir():roots.append(('pillow.libs',vendor))
    for name,root in roots:
        for path in root.rglob('*'):
            if path.is_symlink():raise ValueError('PROCESSING_COMPONENT_SYMLINK')
            native=path.name.endswith(('.so','.dylib','.dll')) or '.so.' in path.name
            if not path.is_file() or path.suffix!='.py' and not native:continue
            total+=path.stat().st_size
            if len(result)>=20000 or total>512*1024**2:raise ValueError('PROCESSING_INVENTORY_EXCEEDED')
            result['processor/'+name+'/'+path.relative_to(root).as_posix()]=path
    if not result:raise ValueError('PROCESSING_INVENTORY_EMPTY')
    result['processing_inventory_source']=Path(__file__).resolve()
    return result
