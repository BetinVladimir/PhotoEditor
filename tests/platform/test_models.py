from pathlib import Path
import hashlib
import sys
from types import SimpleNamespace
import numpy as np
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import canonical_hash
from photo_worker.models.lock import ModelLock,ModelFile
from photo_worker.models.install import ModelError,install_models,verify_model
from photo_worker.models.adapters import factories

def lock_for(data):
    entry={'id':'lraspp_mobilenet_v3_large','architecture':'test-lock-only','weight_id':'fixture','repository':'https://download.pytorch.org/models/','revision':'fixture1','files':[{'path':'weights.bin','bytes':len(data),'sha256':hashlib.sha256(data).hexdigest(),'url':'https://download.pytorch.org/models/fixture.bin'}],'preprocessing_id':'test','packages_lock_hash':'a'*64,'code_license':'CC0-1.0','weights_license':'CC0-1.0','notices':[],'redistribution_policy':'allowed','approved_for_install':True,'backends':{b:{'status':'untested','reason':'fixture','report_sha256':None} for b in ('cpu','mps','cuda')}}
    payload={'schema_version':1,'models':[entry]}
    return ModelLock.model_validate({**payload,'lock_id':canonical_hash(payload)})

def test_offline_install_and_tamper(tmp_path):
    data=b'not a production model; installer fixture'
    lock=lock_for(data);bundle=tmp_path/'bundle';source=bundle/lock.models[0].id;source.mkdir(parents=True);(source/'weights.bin').write_bytes(data)
    target=tmp_path/'models'
    installed=install_models(lock,[lock.models[0].id],target,bundle)
    assert verify_model(installed[lock.models[0].id],lock.models[0])['weights.bin'].read_bytes()==data
    assert install_models(lock,[lock.models[0].id],target,bundle)==installed
    p=installed[lock.models[0].id]/'weights.bin';p.chmod(0o600);p.write_bytes(b'changed')
    with pytest.raises(ModelError,match='HASH_MISMATCH'):install_models(lock,[lock.models[0].id],target,bundle)

def test_reject_path_url_and_unapproved(tmp_path):
    for path,url in [('../escape','https://download.pytorch.org/x'),('x','http://download.pytorch.org/x'),('x','https://127.0.0.1/x'),('/absolute','https://download.pytorch.org/x')]:
        with pytest.raises((ValueError,ValidationError)):ModelFile(path=path,url=url,bytes=1,sha256='a'*64)
    lock=lock_for(b'test');lock.models[0].approved_for_install=False
    with pytest.raises(ModelError,match='NOT_APPROVED'):install_models(lock,[lock.models[0].id],tmp_path/'models')

def test_lock_hash_is_not_trusted():
    lock=lock_for(b'test');raw=lock.model_dump();raw['lock_id']='0'*64
    with pytest.raises(ValidationError,match='LOCK_HASH_MISMATCH'):ModelLock.model_validate(raw)

def test_runtime_factory_rejects_architecture_substitution(tmp_path):
    entry=lock_for(b'test').models[0].model_copy(update={'id':'scunet_color_real_psnr','architecture':'torchscript.realesrgan_x4.v1'})
    path=tmp_path/'model.pth';path.write_bytes(b'test')
    with pytest.raises(ModelError,match='ARCHITECTURE_INVALID'):
        factories()['scunet_color_real_psnr'](entry,{'model.pth':path},'cpu')

def test_pose_factory_requires_exact_architecture_weight_and_preprocessing(tmp_path):
    path=tmp_path/'model.pth';path.write_bytes(b'test')
    base=lock_for(b'test').models[0].model_copy(update={'id':'keypointrcnn_resnet50_fpn'})
    cases=[
        {'architecture':'torchvision.keypointrcnn_resnet50_fpn.latest','weight_id':'KeypointRCNN_ResNet50_FPN_Weights.COCO_V1','preprocessing_id':'keypointrcnn_coco_v1'},
        {'architecture':'torchvision.keypointrcnn_resnet50_fpn.coco_v1','weight_id':'latest','preprocessing_id':'keypointrcnn_coco_v1'},
        {'architecture':'torchvision.keypointrcnn_resnet50_fpn.coco_v1','weight_id':'KeypointRCNN_ResNet50_FPN_Weights.COCO_V1','preprocessing_id':'unknown'},
    ]
    for change in cases:
        with pytest.raises(ModelError,match='ARCHITECTURE_INVALID'):
            factories()['keypointrcnn_resnet50_fpn'](base.model_copy(update=change),{'model.pth':path},'cpu')

def test_sfd_factory_requires_exported_face_alignment_identity(tmp_path):
    path=tmp_path/'model.torchscript';path.write_bytes(b'test')
    base=lock_for(b'test').models[0].model_copy(update={'id':'sfd'})
    cases=[
        {'architecture':'torchscript.face_alignment.sfd.latest','weight_id':'face_alignment.s3fd-619a316812.pth','preprocessing_id':'sfd_long1024_bgr_v1'},
        {'architecture':'torchscript.face_alignment.sfd.v1','weight_id':'latest','preprocessing_id':'sfd_long1024_bgr_v1'},
        {'architecture':'torchscript.face_alignment.sfd.v1','weight_id':'face_alignment.s3fd-619a316812.pth','preprocessing_id':'rgb'},
    ]
    for change in cases:
        with pytest.raises(ModelError,match='ARCHITECTURE_INVALID'):
            factories()['sfd'](base.model_copy(update=change),{'model.torchscript':path},'cpu')

def test_fan_factory_requires_exported_2dfan_identity(tmp_path):
    path=tmp_path/'model.torchscript';path.write_bytes(b'test')
    base=lock_for(b'test').models[0].model_copy(update={'id':'fan2d'})
    cases=[
        {'architecture':'torchscript.face_alignment.fan2d.latest','weight_id':'face_alignment.2DFAN4-cd938726ad.pth.tar','preprocessing_id':'fan2d_crop256_v1'},
        {'architecture':'torchscript.face_alignment.fan2d.v1','weight_id':'latest','preprocessing_id':'fan2d_crop256_v1'},
        {'architecture':'torchscript.face_alignment.fan2d.v1','weight_id':'face_alignment.2DFAN4-cd938726ad.pth.tar','preprocessing_id':'unknown'},
    ]
    for change in cases:
        with pytest.raises(ModelError,match='ARCHITECTURE_INVALID'):
            factories()['fan2d'](base.model_copy(update=change),{'model.torchscript':path},'cpu')


def test_identity512_factory_requires_exact_onnx_contract(tmp_path,monkeypatch):
    path=tmp_path/'identity.onnx';path.write_bytes(b'fixture')
    base=lock_for(b'test').models[0].model_copy(update={
        'id':'identity512','architecture':'onnx.identity512.v2_1',
        'weight_id':'identity512_onnx_v2_1',
        'preprocessing_id':'srgb_nchw_float32_0_1'})
    for change in ({'architecture':'onnx.identity512.latest'},
                   {'weight_id':'unqualified'},
                   {'preprocessing_id':'rgb_uint8'}):
        with pytest.raises(ModelError,match='ARCHITECTURE_INVALID'):
            factories()['identity512'](base.model_copy(update=change),
                                       {'identity.onnx':path},'cuda')

    class Session:
        def __init__(self,path,providers):
            self.providers=providers
        def get_providers(self):return self.providers
        def get_inputs(self):
            return [SimpleNamespace(name='rgb',shape=[1,3,112,112],
                                    type='tensor(float)')]
        def get_outputs(self):
            return [SimpleNamespace(name='embedding',shape=[1,512],
                                    type='tensor(float)')]
        def run(self,names,feeds):
            assert names==['embedding'] and feeds['rgb'].shape==(1,3,112,112)
            return [np.ones((1,512),np.float32)]

    monkeypatch.setitem(sys.modules,'onnxruntime',SimpleNamespace(
        get_available_providers=lambda:['CPUExecutionProvider'],
        InferenceSession=Session))
    model=factories()['identity512'](base,{'identity.onnx':path},'cpu')
    embedding=model.infer(np.full((112,112,3),.5,np.float32))
    assert embedding.shape==(512,)
    assert np.isclose(np.linalg.norm(embedding),1)
    with pytest.raises(ModelError,match='BACKEND_UNAVAILABLE'):
        factories()['identity512'](base,{'identity.onnx':path},'cuda')
    with pytest.raises(ValueError,match='MODEL_INPUT_INVALID'):
        model.infer(np.zeros((112,112,3),np.float32)-1)
