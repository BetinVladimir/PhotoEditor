import ast
from pathlib import Path
from types import SimpleNamespace
import numpy as np
import pytest

source=Path(__file__).resolve().parents[1] / 'scripts' / 'codeformer_arcface_gate.py'
tree=ast.parse(source.read_text())
f=next(x for x in tree.body if isinstance(x,ast.FunctionDef) and x.name=='face_geometry_evidence')
ns={}
exec(compile(ast.Module(body=[f],type_ignores=[]),str(source),'exec'),ns)

@pytest.mark.parametrize('index',[0,1])
def test_original_geometry_is_bound_to_exact_detector_index_without_rescaling(index):
    boxes=np.array([[12.5,21.25,333.5,412.75,.93],[700.25,40.,1100.75,490.5,.87]],dtype=np.float32)
    points=np.array([[[20,30],[300,31],[160,180],[80,300],[240,301]],[[750,70],[1000,71],[880,220],[790,390],[980,391]]],dtype=np.float32)
    helper=SimpleNamespace(det_faces=boxes.copy(),all_landmarks_5=points.copy())
    result=ns['face_geometry_evidence'](helper,index)
    assert result['detector_box_original_xyxy']==boxes[index,:4].tolist()
    assert result['detector_confidence']==float(boxes[index,4])
    assert result['detector_landmarks_original_xy']==points[index].tolist()
    np.testing.assert_array_equal(helper.det_faces,boxes)
    np.testing.assert_array_equal(helper.all_landmarks_5,points)
    import json
    assert json.loads(json.dumps(result,allow_nan=False))==result
