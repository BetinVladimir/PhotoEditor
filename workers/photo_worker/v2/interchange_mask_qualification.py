"""Physically qualify imported opaque MaskArray bytes against canonical canvas."""
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import Canvas
from photo_worker.runtime.arrays import read_array
from .adoption import ArtifactRef,AdoptionInvalid
from .interchange_parse import ParsedInterchange
from .interchange_remap import UnsupportedDependency


@dataclass(frozen=True)
class QualifiedImportedMask:
    reference: ArtifactRef
    canvas_hash: str
    shape: tuple[int,int]


class ImportedMaskQualifier:
    def __init__(self,parsed,canonical_canvases,reader,scratch,*,check_active=lambda:None):
        parsed=ParsedInterchange.model_validate_json(canonical_bytes(parsed))
        self.reader,self.scratch,self.active=reader,scratch,check_active
        self.canvases={};self.inventory={};self.qualified={}
        for digest,canvas in canonical_canvases.items():
            owned=Canvas.model_validate_json(canonical_bytes(canvas))
            if canonical_hash(owned)!=digest:
                raise ValueError('INTERCHANGE_MASK_CANVAS_INSTALLATION')
            self.canvases[digest]=owned
        files={file.path:file for file in parsed.files}
        inventory={item.path:item for item in parsed.manifest.inventory}
        for mask in parsed.manifest.masks:
            item=inventory.get(mask.path);file=files.get(mask.path)
            if (item is None or file is None or item.contract!='MaskArray' or file.contract!='MaskArray'
                    or item.sha256!=mask.sha256 or file.content.sha256!=mask.sha256):
                raise UnsupportedDependency('INTERCHANGE_MASK_INVENTORY')
            reference=ArtifactRef(file.content.artifact_id,file.content.sha256)
            if reference in self.inventory and self.inventory[reference]!=mask.canvas_hash:
                raise UnsupportedDependency('INTERCHANGE_MASK_AMBIGUOUS_CANVAS')
            self.inventory[reference]=mask.canvas_hash

    def __call__(self,contract,reference):
        self.active()
        ref=ArtifactRef(reference.artifact_id,reference.sha256)
        digest=self.inventory.get(ref)
        canvas=self.canvases.get(digest)
        if contract!='MaskArray' or digest is None or canvas is None:
            raise UnsupportedDependency('INTERCHANGE_MASK_CANVAS_UNQUALIFIED')
        if ref in self.qualified:
            return self.qualified[ref]
        item=self.reader.evidence(ref)
        if (item.ref!=ref or item.source!='input' or item.media_type!='application/octet-stream'
                or type(item.bytes) is not int or not 1<=item.bytes<=canvas.width*canvas.height*4+4096):
            raise AdoptionInvalid('INTERCHANGE_MASK_EVIDENCE')
        shape=(canvas.height,canvas.width)
        with self.scratch.reserve(item.bytes) as reservation:
            path=reservation.path('imported-mask','.npy')
            count=0;checksum=sha256()
            iterator=iter(self.reader.iter_bytes(ref,item.bytes))
            try:
                with self.scratch.open_writer(path,reservation) as writer:
                    for block in iterator:
                        self.active()
                        if not isinstance(block,bytes) or not 1<=len(block)<=1<<20 or count+len(block)>item.bytes:
                            raise AdoptionInvalid('INTERCHANGE_MASK_STREAM')
                        if writer.write(block)!=len(block):raise AdoptionInvalid('INTERCHANGE_MASK_SHORT_WRITE')
                        checksum.update(block);count+=len(block)
            finally:
                close=getattr(iterator,'close',None)
                if close is not None:close()
            if count!=item.bytes or checksum.hexdigest()!=ref.sha256:
                raise AdoptionInvalid('INTERCHANGE_MASK_HASH')
            reservation.validate_file(path)
            try:
                with read_array(path,shape,'float32',self.active,unit_interval=True):
                    self.active()
            except ValueError as error:
                raise UnsupportedDependency('INTERCHANGE_MASK_ARRAY_INVALID') from error
        self.active()
        receipt=QualifiedImportedMask(ref,digest,shape)
        self.qualified[ref]=receipt
        return receipt
