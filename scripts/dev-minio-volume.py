#!/usr/bin/env python3
"""Mount a repository-owned, bounded sparse image for macOS development MinIO."""
from pathlib import Path
import json
import plistlib
import subprocess
ROOT=Path(__file__).resolve().parents[1]
work=ROOT/'work'
image=work/'minio-dev.sparseimage'
mount=work/'minio-volume'
marker=work/'minio-volume.json'
identity={'format_version':1,'image':'minio-dev.sparseimage','mount':'minio-volume','size':'2g','volume_name':'PhotoEditorMinioDev'}
def main():
    work.mkdir(exist_ok=True)
    if not image.exists():
        subprocess.run(['hdiutil','create','-size','2g','-fs','APFS','-type','SPARSE','-volname',identity['volume_name'],str(image)],check=True,cwd=ROOT)
        marker.write_text(json.dumps(identity,indent=2)+'\n')
    if image.is_symlink() or not marker.exists() or json.loads(marker.read_text())!=identity:
        raise SystemExit('Refusing unrecognized development volume')
    info=plistlib.loads(subprocess.check_output(['hdiutil','info','-plist']))
    for existing in info.get('images',[]):
        if Path(existing.get('image-path','')).resolve()==image.resolve():
            points=[e.get('mount-point') for e in existing.get('system-entities',[]) if e.get('mount-point')]
            if points==[str(mount)]:return
            raise SystemExit('Development image is attached at an unexpected mount point')
    mount.mkdir(exist_ok=True)
    if mount.is_symlink() or any(mount.iterdir()):raise SystemExit('Development mount point is not empty')
    subprocess.run(['hdiutil','attach','-nobrowse','-mountpoint',str(mount),str(image)],check=True,cwd=ROOT)
if __name__=='__main__':main()
