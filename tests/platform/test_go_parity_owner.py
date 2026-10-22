"""Pure filesystem/lock controls; never invokes Go or a service."""
from contextlib import contextmanager
from pathlib import Path
import fcntl
import os
from types import SimpleNamespace
import tempfile
import subprocess
import sys
import json
import unittest
from unittest.mock import patch
import go_parity_owner as g


class GoParityOwnerControls(unittest.TestCase):
    @contextmanager
    def fixture(self):
        control=Path(__file__).resolve().parents[2]/'work/go-parity-owner-fix-r1'
        control.mkdir(parents=True,exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='go-owner-control-',dir=control) as directory:
            root=Path(directory);(root/'.git').mkdir();(root/'backend').mkdir();(root/'backend/go.mod').write_text(g.MODULE+'\n')
            anchor=root/'tests/platform/test_control.py';anchor.parent.mkdir(parents=True);anchor.write_text('')
            with patch.dict(os.environ,{},clear=True),patch.object(g.shutil,'disk_usage',return_value=SimpleNamespace(free=g.MIN_FREE_BYTES)):
                yield root,anchor

    def test_repository_caches_temp_and_cleanup(self):
        with self.fixture() as (root,anchor):
            with g.go_parity_owner(anchor,{'PHOTO_CONTROL':'actual-test-data'}) as owner:
                self.assertEqual(owner.backend,root/'backend');self.assertEqual(owner.env['GOCACHE'],str(root/'work/go-cache'))
                self.assertEqual(owner.env['GOMODCACHE'],str(root/'.cache/go-mod'))
                self.assertTrue(owner.temporary.is_relative_to(root/'work/tmp'))
                self.assertEqual(owner.env['TMPDIR'],owner.env['GOTMPDIR']);self.assertEqual(owner.env['PHOTO_CONTROL'],'actual-test-data')
                temporary=owner.temporary
            self.assertFalse(temporary.exists())

    def test_sourceview_selects_real_outer_repository_cache(self):
        with self.fixture() as (root,unused):
            view=root/'work/private/source-view';(view/'backend').mkdir(parents=True);(view/'backend/go.mod').write_text(g.MODULE+'\n')
            anchor=view/'tests/platform/test_control.py';anchor.parent.mkdir(parents=True);anchor.write_text('')
            with g.go_parity_owner(anchor) as owner:
                self.assertEqual(owner.backend,view/'backend');self.assertEqual(owner.env['GOCACHE'],str(root/'work/go-cache'))
            fd=os.open(root/'work'/g.LOCK_NAME,os.O_RDWR)
            try:
                fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
                with patch.dict(os.environ,{'PHOTO_NATIVE_OWNER_LOCK_FD':str(fd)}):
                    with self.assertRaisesRegex(RuntimeError,'EXACT_OUTER_GROUP_REQUIRED'):
                        with g.go_parity_owner(anchor):pass
            finally:os.close(fd)

    def test_external_relative_and_symlink_cache_refuse(self):
        with self.fixture() as (root,anchor):
            outside=root/'outside';outside.mkdir();(root/'work').mkdir();(root/'work/alias').symlink_to(outside,target_is_directory=True)
            for value in ['/private/tmp/photoeditor-go-cache','relative-cache',str(root/'work/alias'),str(root/'work/../outside')]:
                with self.subTest(value=value),patch.dict(os.environ,{'GOCACHE':value}):
                    with self.assertRaises((RuntimeError,OSError)):
                        with g.go_parity_owner(anchor):self.fail('unsafe cache entered')

    def test_cache_owner_mode_and_external_extra_env_refuse(self):
        with self.fixture() as (root,anchor):
            path=root/'work/unsafe';path.mkdir(parents=True);path.chmod(0o777)
            with patch.dict(os.environ,{'GOCACHE':str(path)}):
                with self.assertRaisesRegex(RuntimeError,'CACHE_OWNER_OR_MODE'):
                    with g.go_parity_owner(anchor):pass
            with self.assertRaisesRegex(RuntimeError,'EXTRA_ENV'):
                with g.go_parity_owner(anchor,{'GOCACHE':str(root/'work/safe')}):pass

    def test_floor_refuses_before_cache_creation(self):
        with self.fixture() as (root,anchor),patch.object(g.shutil,'disk_usage',return_value=SimpleNamespace(free=g.MIN_FREE_BYTES-1)):
            with self.assertRaisesRegex(RuntimeError,'DISK_BELOW_16_GIB'):
                with g.go_parity_owner(anchor):pass
            self.assertFalse((root/'work').exists())

    def test_busy_canonical_lane_refuses(self):
        with self.fixture() as (root,anchor):
            (root/'work').mkdir();path=root/'work'/g.LOCK_NAME
            fd=os.open(path,os.O_CREAT|os.O_RDWR,0o600)
            try:
                fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
                with self.assertRaisesRegex(RuntimeError,'EXCLUSIVE_LANE_BUSY'):
                    with g.go_parity_owner(anchor):pass
            finally:os.close(fd)

    def test_inherited_owner_preserved_and_wrong_unheld_fd_rejected(self):
        with self.fixture() as (root,anchor):
            (root/'work').mkdir();path=root/'work'/g.LOCK_NAME;fd=os.open(path,os.O_CREAT|os.O_RDWR,0o600)
            try:
                with patch.dict(os.environ,{'PHOTO_NATIVE_OWNER_LOCK_FD':str(fd)}):
                    with self.assertRaisesRegex(RuntimeError,'INHERITED_LOCK_NOT_HELD'):
                        with g.go_parity_owner(anchor):pass
                    fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
                    with self.assertRaisesRegex(RuntimeError,'EXACT_OUTER_GROUP_REQUIRED'):
                        with g.go_parity_owner(anchor):pass
                    # Test inherited FD ownership independently of launch mode.
                    with g._exclusive(root,dict(os.environ)):pass
                    probe=os.open(path,os.O_RDWR)
                    try:
                        with self.assertRaises(BlockingIOError):fcntl.flock(probe,fcntl.LOCK_EX|fcntl.LOCK_NB)
                    finally:os.close(probe)
            finally:os.close(fd)

    def test_foreign_owner_descriptor_and_lock_symlink_refuse(self):
        with self.fixture() as (root,anchor):
            (root/'work').mkdir();path=root/'work'/g.LOCK_NAME
            foreign=root/'work/foreign.lock';fd=os.open(foreign,os.O_CREAT|os.O_RDWR,0o600)
            path.write_bytes(b'');path.chmod(0o600)
            try:
                with patch.dict(os.environ,{'PHOTO_NATIVE_OWNER_LOCK_FD':str(fd)}):
                    with self.assertRaisesRegex(RuntimeError,'EXACT_OWNER_LOCK_REQUIRED'):
                        with g.go_parity_owner(anchor):pass
                path.unlink();path.symlink_to(foreign)
                with self.assertRaises(OSError):
                    with g.go_parity_owner(anchor):pass
            finally:os.close(fd)

    def test_owned_short_process_output(self):
        with self.fixture() as (root,anchor):
            with g.go_parity_owner(anchor) as owner:
                result=owner.run([sys.executable,'-c','print("owned-positive")'],timeout=2)
                self.assertEqual(result.returncode,0);self.assertEqual(result.stdout.strip(),'owned-positive')

    def test_timeout_reaps_real_child_grandchild_before_lane_release(self):
        with self.fixture() as (root,anchor):
            with g.go_parity_owner(anchor) as owner:
                record=owner.temporary/'children.json'
                script="""import subprocess,signal,sys,os,json
child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'])
def stop(signum,frame):
    child.wait(timeout=1)
    raise SystemExit(0)
signal.signal(signal.SIGTERM,stop)
with open(sys.argv[1],'w') as out:json.dump({'parent':os.getpid(),'grandchild':child.pid,'group':os.getpgrp()},out)
signal.pause()
"""
                with self.assertRaises(subprocess.TimeoutExpired):owner.run([sys.executable,'-c',script,str(record)],timeout=.5)
                observed=json.loads(record.read_text());self.assertEqual(observed['group'],observed['parent'])
                self.assertFalse(g._group_exists(observed['group']))
                for pid in (observed['parent'],observed['grandchild']):
                    with self.assertRaises(ProcessLookupError):os.kill(pid,0)
                probe=os.open(root/'work'/g.LOCK_NAME,os.O_RDWR)
                try:
                    with self.assertRaises(BlockingIOError):fcntl.flock(probe,fcntl.LOCK_EX|fcntl.LOCK_NB)
                finally:os.close(probe)
            # Lane is reusable only after the actual owned process group is gone.
            with g.go_parity_owner(anchor):pass

    def test_source_alias_and_missing_repository_refuse(self):
        with self.fixture() as (root,anchor):
            alias=anchor.parent/'alias.py';alias.symlink_to(anchor)
            with self.assertRaisesRegex(RuntimeError,'SOURCE_ALIAS'):g.repository_root(alias)
            with self.assertRaisesRegex(RuntimeError,'REAL_REPOSITORY_REQUIRED'):g.repository_root(Path('/nonexistent-go-parity-sentinel/test.py'))


if __name__=='__main__':unittest.main()
