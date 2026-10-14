from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, MaskRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.adoption import ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.interchange_recipe_arrays import PreparedImportArrayQualifier
from test_interchange_import_stage import adopted_fixture


@pytest.mark.parametrize('mode', ['valid_rgb', 'valid_mask', 'dtype', 'shape', 'nan', 'range', 'hash', 'unwitnessed', 'foreign'])
def test_prepared_arrays_enforce_physical_layout_and_witness(tmp_path, mode):
    _, sources, _, raw_image = adopted_fixture()
    image = WorkingImage.model_validate_json(raw_image)
    h, w = image.canvas.height, image.canvas.width
    rgb = mode == 'valid_rgb'
    array = np.full((h, w, 3) if rgb else (h, w), 2.5 if rgb else .5, dtype=np.float32)
    if mode == 'dtype':
        array = array.astype(np.float64)
    elif mode == 'shape':
        array = np.full((w, h), .5, dtype=np.float32)
    elif mode == 'nan':
        array[0, 0] = np.nan
    elif mode == 'range':
        array[0, 0] = 1.1
    output = BytesIO();np.save(output, array, allow_pickle=False)
    raw = output.getvalue()
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    if rgb:
        image.pixels = ref
    master = sources.entries[0].master.model_copy(deep=True)
    master.sha256 = sha256(canonical_bytes(image)).hexdigest()
    binding = VersionBinding(asset_version_id=sources.entries[0].asset_version_id,
                             source=master, canvas_hash=canonical_hash(image.canvas))
    mask = MaskRef(artifact=ref, canvas=image.canvas, transform_chain=image.transform_chain,
        object_id=None, selector='manual', confidence=1, model_lock_id=None, protected=False)
    class Reader:
        calls = 0
        def evidence(self, reference):
            return ArtifactEvidence(reference, 'application/octet-stream', len(raw), None, 'input')
        def iter_bytes(self, reference, maximum):
            self.calls += 1
            yield b'x' + raw[1:] if mode == 'hash' else raw
    reader = Reader()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        qualifier = PreparedImportArrayQualifier(canonical_images=[(binding, image)], reader=reader,
            scratch=scratch, max_file_bytes=1 << 20)
        if not rgb and mode != 'unwitnessed':
            qualifier.register_mask(binding, mask)
        if mode == 'foreign':
            binding.asset_version_id = uuid4()
        if mode in ('valid_rgb', 'valid_mask'):
            assert qualifier(ref, 'WorkingPixels' if rgb else 'MaskArray', binding) is True
            assert reader.calls == 1
        else:
            with pytest.raises((AdoptionInvalid, ValueError)):
                qualifier(ref, 'MaskArray', binding)
            if mode in ('unwitnessed', 'foreign'):
                assert reader.calls == 0


def test_shared_correction_closure_uses_each_canonical_images_own_shape(tmp_path):
    _, sources, _, raw_image = adopted_fixture()
    first = WorkingImage.model_validate_json(raw_image)
    second = first.model_copy(deep=True)
    second.canvas.width, second.canvas.height = 5, 7
    output = BytesIO();np.save(output, np.full((7, 5, 3), 2.5, dtype=np.float32), allow_pickle=False)
    raw = output.getvalue()
    second.pixels = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    def bind(image):
        return VersionBinding(asset_version_id=uuid4(), source=ArtifactRef(artifact_id=uuid4(),
            sha256=sha256(canonical_bytes(image)).hexdigest()), canvas_hash=canonical_hash(image.canvas))
    first_binding, second_binding = bind(first), bind(second)
    class Reader:
        calls = 0
        def evidence(self, reference):
            return ArtifactEvidence(reference, 'application/octet-stream', len(raw), None, 'input')
        def iter_bytes(self, reference, maximum):
            self.calls += 1
            yield raw
    reader = Reader()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        qualifier = PreparedImportArrayQualifier(canonical_images=[(first_binding, first), (second_binding, second)],
            reader=reader, scratch=scratch, max_file_bytes=1 << 20)
        assert qualifier(second.pixels, 'WorkingPixels', first_binding) is True
        assert reader.calls == 1
        with pytest.raises(AdoptionInvalid, match='UNWITNESSED'):
            qualifier(second.pixels, 'MaskArray', first_binding)
        foreign = first_binding.model_copy(deep=True);foreign.asset_version_id = uuid4()
        with pytest.raises(AdoptionInvalid, match='UNWITNESSED'):
            qualifier(second.pixels, 'WorkingPixels', foreign)
        assert reader.calls == 1


def test_canonical_pixel_identity_cannot_claim_two_shapes(tmp_path):
    _, sources, _, raw = adopted_fixture()
    first = WorkingImage.model_validate_json(raw);second = first.model_copy(deep=True)
    second.canvas.width += 1
    def bind(image):
        return VersionBinding(asset_version_id=uuid4(), source=ArtifactRef(artifact_id=uuid4(),
            sha256=sha256(canonical_bytes(image)).hexdigest()), canvas_hash=canonical_hash(image.canvas))
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        with pytest.raises(AdoptionInvalid, match='CONFLICT'):
            PreparedImportArrayQualifier(canonical_images=[(bind(first), first), (bind(second), second)],
                reader=object(), scratch=scratch, max_file_bytes=1 << 20)


@pytest.mark.parametrize('contract', ['DenseWarpArray', 'DenseWarpField'])
@pytest.mark.parametrize('mode', ['valid', 'wrong_shape', 'nan'])
def test_dense_warp_uses_occurrence_output_grid_before_final_resize(tmp_path, mode, contract):
    from photo_worker.contracts.images import DenseWarpTransform, ResizeTransform, Rect, TransformChain
    _, _, raw_image = adopted_fixture()
    image = WorkingImage.model_validate_json(raw_image)
    intermediate = image.canvas.model_copy(deep=True)
    intermediate.width = image.canvas.width + 1
    intermediate.height = image.canvas.height + 1
    shape = (intermediate.height, intermediate.width, 2)
    if mode == 'wrong_shape':
        shape = (image.canvas.height, image.canvas.width, 2)
    values = np.full(shape, -2.5, dtype=np.float32)
    if mode == 'nan':
        values[0, 0, 0] = np.nan
    output = BytesIO();np.save(output, values, allow_pickle=False);raw = output.getvalue()
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    warp = DenseWarpTransform(kind='dense_warp', from_canvas=image.canvas, to_canvas=intermediate,
        invertibility='partial', valid_region=Rect(x=0,y=0,width=intermediate.width,height=intermediate.height),
        inverse_field=ref)
    resize = ResizeTransform(kind='resize', from_canvas=intermediate, to_canvas=image.canvas,
        invertibility='partial', valid_region=Rect(x=0,y=0,width=image.canvas.width,height=image.canvas.height),
        pixel_centers='half_pixel')
    image.transform_chain = TransformChain.create([warp, resize])
    binding = VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(image)).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    class Reader:
        def evidence(self, reference):
            return ArtifactEvidence(reference, 'application/octet-stream', len(raw), None, 'input')
        def iter_bytes(self, reference, maximum):
            yield raw
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        qualifier = PreparedImportArrayQualifier(canonical_images=[(binding,image)],reader=Reader(),
            scratch=scratch,max_file_bytes=1 << 20)
        if mode == 'valid':
            assert qualifier(ref,contract,binding) is True
        else:
            with pytest.raises((ValueError,AdoptionInvalid)):
                qualifier(ref,contract,binding)


@pytest.mark.parametrize('contract,accepted', [('MaskArray', True), ('MaskPixels', True),
    ('PixelArray', False), ('WorkingAlpha', False), ('DenseWarpField', False), ('NpyArray', False)])
def test_transport_role_aliases_preserve_mask_witness_authority(tmp_path, contract, accepted):
    _, _, raw_image = adopted_fixture()
    image = WorkingImage.model_validate_json(raw_image)
    values = np.full((image.canvas.height,image.canvas.width), .5, dtype=np.float32)
    output = BytesIO();np.save(output, values, allow_pickle=False);raw = output.getvalue()
    ref = ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    binding = VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(image)).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    mask = MaskRef(artifact=ref,canvas=image.canvas,transform_chain=image.transform_chain,
        object_id=None,selector='manual',confidence=1,model_lock_id=None,protected=False)
    class Reader:
        calls = 0
        def evidence(self, reference):
            self.calls += 1
            return ArtifactEvidence(reference,'application/octet-stream',len(raw),None,'input')
        def iter_bytes(self, reference, maximum):
            yield raw
    reader = Reader()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1 << 20,min_free_bytes=0) as scratch:
        qualifier = PreparedImportArrayQualifier(canonical_images=[(binding,image)],reader=reader,
            scratch=scratch,max_file_bytes=1 << 20)
        qualifier.register_mask(binding,mask)
        if accepted:
            assert qualifier(ref,contract,binding) is True
        else:
            with pytest.raises(AdoptionInvalid,match='UNWITNESSED'):
                qualifier(ref,contract,binding)
            assert reader.calls == 0


def test_normative_mask_array_alpha_requires_canonical_image_witness(tmp_path):
    _, _, raw_image = adopted_fixture()
    image = WorkingImage.model_validate_json(raw_image)
    output = BytesIO();np.save(output,np.ones((image.canvas.height,image.canvas.width),dtype=np.float32),allow_pickle=False)
    raw = output.getvalue();ref = ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    image.alpha = ref
    binding = VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(image)).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    class Reader:
        def evidence(self, reference):
            return ArtifactEvidence(reference,'application/octet-stream',len(raw),None,'input')
        def iter_bytes(self, reference, maximum):
            yield raw
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1 << 20,min_free_bytes=0) as scratch:
        qualifier = PreparedImportArrayQualifier(canonical_images=[(binding,image)],reader=Reader(),scratch=scratch,max_file_bytes=1 << 20)
        assert qualifier(ref,'MaskArray',binding) is True
        assert qualifier(ref,'WorkingAlpha',binding) is True
        with pytest.raises(AdoptionInvalid,match='UNWITNESSED'):
            qualifier(ref,'PixelArray',binding)


@pytest.mark.parametrize('changed', ['none', 'bytes', 'media', 'source', 'role', 'ref'])
def test_array_rechecks_evidence_after_physical_io(tmp_path, changed):
    _, _, raw_image = adopted_fixture()
    image = WorkingImage.model_validate_json(raw_image)
    output = BytesIO();np.save(output,np.ones((image.canvas.height,image.canvas.width,3),dtype=np.float32),allow_pickle=False)
    raw = output.getvalue();image.pixels = ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    binding = VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(image)).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    class Reader:
        calls = 0
        streamed = False
        def evidence(self, reference):
            self.calls += 1
            values = dict(ref=reference,media_type='application/octet-stream',bytes=len(raw),role=None,source='input')
            if self.streamed and changed != 'none':
                values[changed] = {'bytes':len(raw)+1,'media':'application/x-npy','source':'output',
                    'role':'different','ref':type(reference)(uuid4(),reference.sha256)}[changed]
                if changed == 'media':
                    values['media_type'] = values.pop('media')
            return ArtifactEvidence(**values)
        def iter_bytes(self, reference, maximum):
            yield raw
            self.streamed = True
    reader = Reader()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1 << 20,min_free_bytes=0) as scratch:
        qualifier = PreparedImportArrayQualifier(canonical_images=[(binding,image)],reader=reader,
            scratch=scratch,max_file_bytes=1 << 20)
        if changed == 'none':
            assert qualifier(image.pixels,'WorkingPixels',binding) is True
        else:
            with pytest.raises(AdoptionInvalid,match='EVIDENCE_CHANGED'):
                qualifier(image.pixels,'WorkingPixels',binding)
        assert reader.calls == 2


@pytest.mark.parametrize('version', [1,2])
@pytest.mark.parametrize('role', ['rgb','mask'])
def test_prepared_array_accepts_maximum_header_plus_npy_prefix(tmp_path, version, role):
    import struct
    from photo_worker.runtime.arrays import MAX_HEADER
    _,_,_,raw_image=adopted_fixture()
    image=WorkingImage.model_validate_json(raw_image)
    h,w=image.canvas.height,image.canvas.width
    shape=(h,w,3) if role=='rgb' else (h,w)
    values=np.full(shape,.5,dtype=np.float32)
    text=repr({'descr':'<f4','fortran_order':False,'shape':shape}).encode('ascii')
    header=text+b' '*(MAX_HEADER-len(text)-1)+b'\n'
    prefix=b'\x93NUMPY'+bytes((version,0))+struct.pack('<H' if version==1 else '<I',len(header))
    raw=prefix+header+values.tobytes()
    reference=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    if role=='rgb':image.pixels=reference
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(image)).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    class Reader:
        def evidence(self,ref):return ArtifactEvidence(ref,'application/x-npy',len(raw),None,'input')
        def iter_bytes(self,ref,maximum):yield raw
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        qualifier=PreparedImportArrayQualifier(canonical_images=[(binding,image)],reader=Reader(),scratch=scratch,max_file_bytes=1<<20)
        if role=='mask':
            qualifier.register_mask(binding,MaskRef(artifact=reference,canvas=image.canvas,
                transform_chain=image.transform_chain,object_id=None,selector='manual',confidence=1,
                model_lock_id=None,protected=False))
        assert qualifier(reference,'WorkingPixels' if role=='rgb' else 'MaskPixels',binding) is True


@pytest.mark.parametrize('mode', ['valid','unknown_label','dtype','unwitnessed','source','role'])
def test_defect_label_binary_requires_qualified_proposal_and_exact_physical_labels(tmp_path,mode):
    from photo_worker.contracts.defects import DefectCandidates
    _,_,_,raw_image=adopted_fixture()
    image=WorkingImage.model_validate_json(raw_image)
    values=np.zeros((image.canvas.height,image.canvas.width),dtype=np.uint16)
    if mode=='unknown_label':values[0,0]=1
    if mode=='dtype':values=values.astype(np.float32)
    output=BytesIO();np.save(output,values,allow_pickle=False);raw=output.getvalue()
    reference=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(image)).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    proposal=DefectCandidates(image=binding.source,source_pixels=image.pixels,label_map=reference,
        canvas=image.canvas,transform_chain=image.transform_chain,candidates=[])
    class Reader:
        opens=0
        def evidence(self,ref):return ArtifactEvidence(ref,'application/octet-stream',len(raw),None,'input')
        def iter_bytes(self,ref,maximum):self.opens+=1;yield raw
    reader=Reader()
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1<<20,min_free_bytes=0) as scratch:
        qualifier=PreparedImportArrayQualifier(canonical_images=[(binding,image)],reader=reader,scratch=scratch,max_file_bytes=1<<20)
        if mode=='source':
            proposal.image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
            with pytest.raises(AdoptionInvalid,match='DEFECT_SOURCE'):qualifier.register_defect_proposal(binding,proposal)
            assert reader.opens==0
            return
        if mode!='unwitnessed':qualifier.register_defect_proposal(binding,proposal)
        if mode=='valid':assert qualifier(reference,'DefectLabelMap',binding) is True
        else:
            with pytest.raises(ValueError):qualifier(reference,'MaskPixels' if mode=='role' else 'DefectLabelMap',binding)
        if mode in ('unwitnessed','role'):assert reader.opens==0


@pytest.mark.parametrize('mode', ['shared_valid', 'shared_missing_label', 'owned_duplicate'])
def test_shared_defect_labels_verify_every_owned_proposal(tmp_path, mode):
    from photo_worker.contracts.defects import DefectCandidates, DefectCandidate, candidate_id
    from photo_worker.contracts.images import Rect
    _, _, _, raw_image = adopted_fixture()
    image = WorkingImage.model_validate_json(raw_image)
    values = np.zeros((image.canvas.height, image.canvas.width), dtype=np.uint16)
    values[0, 0] = 1
    output = BytesIO(); np.save(output, values, allow_pickle=False); raw = output.getvalue()
    reference = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    binding = VersionBinding(asset_version_id=uuid4(), source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(image)).hexdigest()), canvas_hash=canonical_hash(image.canvas))
    bbox = Rect(x=0, y=0, width=1, height=1)
    candidate = DefectCandidate(id=candidate_id(image.pixels, reference, 1, bbox, 1),
        label=1, bbox=bbox, diameter=1, score=.5)
    proposal = DefectCandidates(image=binding.source, source_pixels=image.pixels, label_map=reference,
        canvas=image.canvas, transform_chain=image.transform_chain, candidates=[candidate])
    class Reader:
        def evidence(self, ref): return ArtifactEvidence(ref, 'application/x-npy', len(raw), None, 'input')
        def iter_bytes(self, ref, maximum): yield raw
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1 << 20, min_free_bytes=0) as scratch:
        qualifier = PreparedImportArrayQualifier(canonical_images=[(binding, image)], reader=Reader(),
            scratch=scratch, max_file_bytes=1 << 20)
        qualifier.register_defect_proposal(binding, proposal)
        qualifier.register_defect_proposal(binding, proposal)
        assert qualifier.defect_proposal_count == 1
        if mode == 'owned_duplicate':
            # Caller mutation must not replace the retained physical inventory.
            proposal.candidates.clear()
        else:
            second = proposal.model_copy(deep=True)
            second.candidates[0].score = .75
            if mode == 'shared_missing_label': second.candidates.clear()
            qualifier.register_defect_proposal(binding, second)
            assert qualifier.defect_proposal_count == 2
        if mode == 'shared_missing_label':
            with pytest.raises(ValueError, match='UNKNOWN_DEFECT_LABEL'):
                qualifier(reference, 'DefectLabelMap', binding)
        else:
            assert qualifier(reference, 'DefectLabelMap', binding) is True


@pytest.mark.parametrize('size', [33, 65])
@pytest.mark.parametrize('mode', ['valid', 'dtype', 'shape', 'range', 'nan', 'unwitnessed', 'source', 'changed_metadata'])
def test_absolute_lut_uses_resolved_witness_and_production_reader(tmp_path, mode, size):
    import json
    from pathlib import Path
    from photo_worker.contracts.resolved import ResolvedStyle
    _, _, _, raw_image = adopted_fixture()
    image = WorkingImage.model_validate_json(raw_image)
    binding = VersionBinding(asset_version_id=uuid4(), source=ArtifactRef(artifact_id=uuid4(),
        sha256=sha256(canonical_bytes(image)).hexdigest()), canvas_hash=canonical_hash(image.canvas))
    values = np.full((size, size, size, 3), .5, dtype=np.float32)
    if mode == 'dtype': values = values.astype(np.float64)
    if mode == 'shape': values = values[:, :, :, :2]
    if mode == 'range': values[0, 0, 0, 0] = 1.5
    if mode == 'nan': values[0, 0, 0, 0] = np.nan
    output = BytesIO(); np.save(output, values, allow_pickle=False); raw = output.getvalue()
    reference = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    fixture = Path(__file__).resolve().parents[2] / 'backend/internal/review/testdata/resolved-binding-python.json'
    value = json.loads(fixture.read_bytes())
    value['mode'] = 'group_fixed'; value['reasons'] = []; value['needs_review'] = False
    value['params']['group_prototype_id'] = 'group'
    value['params']['image'] = json.loads(canonical_bytes(binding.source))
    value['absolute_lut'] = dict(artifact=json.loads(canonical_bytes(reference)), size=size,
        dtype='float32', order='RGB_C', profile_id='work_rec2020_gamma24_v1')
    value['hash'] = canonical_hash({key: item for key, item in value.items() if key != 'hash'})
    resolved = ResolvedStyle.model_validate(value)
    class Reader:
        opens = 0
        observations = 0
        def evidence(self, ref):
            self.observations += 1
            length = len(raw) + (1 if mode == 'changed_metadata' and self.observations > 1 else 0)
            return ArtifactEvidence(ref, 'application/x-npy', length, None, 'input')
        def iter_bytes(self, ref, maximum):
            self.opens += 1
            for start in range(0, len(raw), 1 << 20): yield raw[start:start + (1 << 20)]
    reader = Reader()
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 16 << 20, min_free_bytes=0) as scratch:
        qualifier = PreparedImportArrayQualifier(canonical_images=[(binding, image)], reader=reader,
            scratch=scratch, max_file_bytes=16 << 20)
        if mode == 'source':
            resolved.params.image = ArtifactRef(artifact_id=uuid4(), sha256='a' * 64)
            resolved.hash = canonical_hash(resolved.model_dump(mode='json', exclude={'hash'}))
            with pytest.raises(AdoptionInvalid, match='LUT_SOURCE'): qualifier.register_lut(binding, resolved)
            assert reader.opens == 0
            return
        if mode != 'unwitnessed': qualifier.register_lut(binding, resolved)
        if mode == 'valid': assert qualifier(reference, 'AbsoluteLUTArray', binding) is True
        else:
            with pytest.raises(ValueError): qualifier(reference, 'AbsoluteLUTArray', binding)
        if mode == 'unwitnessed': assert reader.opens == 0
