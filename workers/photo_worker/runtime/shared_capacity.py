"""Optional shared CPU/archive node accounting; never changes individual defaults.

The separate configuration SHA is an operator launch pin. Existing database
capacity-policy SHA does not encode the parent path/margin or attest cgroups.
"""
from __future__ import annotations
from dataclasses import dataclass
from pathlib import Path
import os,stat
from typing import Mapping
from photo_worker.contracts.base import canonical_hash
from .capacity import Capacity

_CGROUP_ROOT=Path('/sys/fs/cgroup')
_PROC_CGROUP=Path('/proc/self/cgroup')
_MOUNTINFO=Path('/proc/self/mountinfo')
_KEYS=('PHOTO_SHARED_CAPACITY_PARENT','PHOTO_SHARED_CAPACITY_RAM_BYTES',
       'PHOTO_SHARED_CAPACITY_IDLE_MARGIN_BYTES','PHOTO_SHARED_CAPACITY_CONFIG_SHA256')

@dataclass(frozen=True)
class SharedParentConfig:
    parent:Path
    ram_bytes:int
    idle_margin_bytes:int
    capacity_policy_sha256:str

    def __post_init__(self):
        if (not isinstance(self.parent,Path) or not self.parent.is_absolute() or '..' in self.parent.parts
                or any(type(n)is not int for n in (self.ram_bytes,self.idle_margin_bytes))
                or not 0<self.idle_margin_bytes<self.ram_bytes<=2**53-1
                or not isinstance(self.capacity_policy_sha256,str) or len(self.capacity_policy_sha256)!=64
                or any(c not in '0123456789abcdef' for c in self.capacity_policy_sha256)):
            raise ValueError('SHARED_CAPACITY_INVALID_CONFIG')

    @property
    def sha256(self)->str:
        return canonical_hash(dict(schema_version=1,parent=str(self.parent),ram_bytes=self.ram_bytes,
            idle_margin_bytes=self.idle_margin_bytes,capacity_policy_sha256=self.capacity_policy_sha256))


def _count(value:str)->int:
    if not isinstance(value,str) or not value or len(value)>16 or not value.isascii() or not value.isdigit():
        raise ValueError('SHARED_CAPACITY_INVALID_COUNT')
    result=int(value)
    if result>2**53-1:raise ValueError('SHARED_CAPACITY_INVALID_COUNT')
    return result


def from_environment(env:Mapping[str,str],*,resource_class:str,backend:str,policy)->SharedParentConfig|None:
    present=[k in env for k in _KEYS]
    if not any(present):return None
    if not all(present) or not all(env.get(k) for k in _KEYS):raise ValueError('SHARED_CAPACITY_CONFIG_INCOMPLETE')
    if resource_class not in ('cpu','archive') or backend!='cpu' or policy is None:
        raise ValueError('SHARED_CAPACITY_REQUIRES_CPU_OR_ARCHIVE_POLICY')
    raw=env[_KEYS[0]]
    if not isinstance(raw,str):raise ValueError('SHARED_CAPACITY_INVALID_PARENT')
    parent=Path(raw)
    if not parent.is_absolute() or str(parent)!=raw or '..' in parent.parts:
        raise ValueError('SHARED_CAPACITY_INVALID_PARENT')
    budget=_count(env[_KEYS[1]]);margin=_count(env[_KEYS[2]])
    if not 0<margin<budget or policy.control_ram_budget_bytes!=budget or policy.photo_ram_budget_bytes!=budget:
        raise ValueError('SHARED_CAPACITY_POLICY_BUDGET_MISMATCH')
    config=SharedParentConfig(parent,budget,margin,policy.sha256)
    if env[_KEYS[3]]!=config.sha256:raise ValueError('SHARED_CAPACITY_CONFIG_SHA_MISMATCH')
    # Reject invalid installations before startup, not after queue consumption.
    _state(config)
    return config


def _assert_mount()->None:
    for line in _MOUNTINFO.read_text().splitlines():
        left,sep,right=line.partition(' - ')
        fields=left.split()
        if sep and len(fields)>4 and fields[4]==str(_CGROUP_ROOT) and right.split()[0]=='cgroup2':return
    raise ValueError('SHARED_CAPACITY_CGROUP2_REQUIRED')


def _membership()->Path:
    lines=_PROC_CGROUP.read_text().splitlines()
    matches=[line[3:] for line in lines if line.startswith('0::')]
    if len(matches)!=1:raise ValueError('SHARED_CAPACITY_UNIFIED_MEMBERSHIP_REQUIRED')
    raw=matches[0];path=Path(raw)
    if not path.is_absolute() or str(path)!=raw or '..' in path.parts:
        raise ValueError('SHARED_CAPACITY_MEMBERSHIP_INVALID')
    return path


def _trusted(st)->None:
    if st.st_uid not in (0,os.geteuid()) or st.st_mode&0o022:
        raise ValueError('SHARED_CAPACITY_UNTRUSTED_OWNER_OR_MODE')


def _read_count(directory:int,name:str)->int:
    fd=os.open(name,os.O_RDONLY|os.O_NOFOLLOW,dir_fd=directory)
    try:
        st=os.fstat(fd);_trusted(st)
        if not stat.S_ISREG(st.st_mode):raise ValueError('SHARED_CAPACITY_INVALID_CONTROL')
        raw=os.read(fd,64)
        if len(raw)==64:raise ValueError('SHARED_CAPACITY_OVERSIZED_CONTROL')
        return _count(raw.decode('ascii').strip())
    finally:os.close(fd)


def _state(config:SharedParentConfig)->tuple[int,int]:
    _assert_mount();current=_membership()
    try:relative=config.parent.relative_to(_CGROUP_ROOT)
    except ValueError:raise ValueError('SHARED_CAPACITY_PARENT_OUTSIDE_CGROUP') from None
    # A shared parent must actually contain this process's CHILD cgroup.
    parent_member=Path('/')/relative
    if not relative.parts or current==parent_member or not current.is_relative_to(parent_member):
        raise ValueError('SHARED_CAPACITY_PARENT_NOT_ANCESTOR')
    directory=os.open(_CGROUP_ROOT,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        _trusted(os.fstat(directory))
        for part in relative.parts:
            child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=directory)
            os.close(directory);directory=child;_trusted(os.fstat(directory))
        maximum=_read_count(directory,'memory.max');usage=_read_count(directory,'memory.current')
        swap=_read_count(directory,'memory.swap.max')
        if maximum!=config.ram_bytes or swap!=0 or usage>maximum:
            raise ValueError('SHARED_CAPACITY_PARENT_LIMIT_DRIFT')
        if _read_count(directory,'memory.max')!=maximum or _read_count(directory,'memory.swap.max')!=0:
            raise ValueError('SHARED_CAPACITY_PARENT_LIMIT_CHANGED')
        if _membership()!=current:raise ValueError('SHARED_CAPACITY_MEMBERSHIP_CHANGED')
        return maximum,usage
    finally:os.close(directory)


def cap_measurement(config:SharedParentConfig,measured:Capacity)->Capacity:
    if not isinstance(measured,Capacity) or any(type(n)is not int or not 0<=n<=2**53-1
            for n in (measured.ram_bytes,measured.scratch_bytes,measured.vram_bytes)) or measured.vram_bytes!=0:
        raise ValueError('SHARED_CAPACITY_INVALID_MEASUREMENT')
    maximum,usage=_state(config)
    # Deliberately conservative with SQL reservations: never increase a sample
    # and never advertise a child's unrelated cap as shared-node availability.
    remaining=max(0,maximum-usage-config.idle_margin_bytes)
    return Capacity(min(measured.ram_bytes,remaining),measured.scratch_bytes,0)
