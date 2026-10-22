"""Adapter-only geometry tests; no model import/loading or person attribution."""
import ast
from contextlib import nullcontext
from pathlib import Path
from types import SimpleNamespace

import cv2
import numpy as np
import pytest

source = Path(__file__).resolve().parents[1] / 'scripts' / 'codeformer_arcface_gate.py'
tree = ast.parse(source.read_text())
function = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == 'detect_original_geometry')
namespace = dict(cv2=cv2, np=np, torch=SimpleNamespace(no_grad=nullcontext))
exec(compile(ast.Module(body=[function], type_ignores=[]), str(source), 'exec'), namespace)
detect = namespace['detect_original_geometry']

class Helper:
    def __init__(self, rows):
        self.rows = rows
        self.det_faces = []
        self.all_landmarks_5 = []
        self.face_detector = SimpleNamespace(detect_faces=self.detector)
    def read_image(self, image):
        self.input_img = image
        if min(image.shape[:2]) < 512:
            f = 512 / min(image.shape[:2])
            self.input_img = cv2.resize(image, (0, 0), fx=f, fy=f, interpolation=cv2.INTER_LINEAR)
    def detector(self, image):
        self.detected_shape = image.shape
        h, w = image.shape[:2]
        # Fixture geometry is normalized, confidence is deliberately invariant.
        factors = np.array([w,h,w,h,1,w,h,w,h,w,h,w,h,w,h])
        return np.array(self.rows, dtype=np.float32).reshape(-1,15) * factors

ROW = [.1,.1,.9,.9,.93,.3,.3,.7,.3,.5,.5,.35,.7,.65,.7]

@pytest.mark.parametrize('hw', [(320,331),(454,424),(376,392),(1579,1568),(1811,1785)])
def test_geometry_maps_to_original_before_warp_and_score_is_unchanged(hw):
    h, w = hw
    image = np.zeros((h,w,3), dtype=np.uint8)
    helper = Helper([ROW])
    assert detect(helper, image) == 1
    assert helper.input_img.shape == image.shape
    assert np.array_equal(helper.input_img, image)
    np.testing.assert_allclose(helper.det_faces[0], [.1*w,.1*h,.9*w,.9*h,.93], rtol=1e-6)
    np.testing.assert_allclose(helper.all_landmarks_5[0][0], [.3*w,.3*h], rtol=1e-6)
    # Detector retains vendor initial enlargement and integer resize dimensions.
    factor = max(1, 512/min(h,w))
    wh, ww = round(h*factor), round(w*factor)
    scale = 800/min(wh,ww)
    assert helper.detected_shape[:2] == (int(wh*scale),int(ww*scale))


def test_eye_filter_uses_two_eye_xy_pairs_not_eye_to_nose_columns():
    row = ROW.copy()
    row[5:11] = [.3,.3,.301,.3,.8,.8]
    helper = Helper([row])
    assert detect(helper, np.zeros((100,100,3),dtype=np.uint8)) == 0
    assert helper.det_faces == []


def test_empty_detector_preserves_original_canvas():
    helper = Helper([])
    assert detect(helper,np.zeros((320,331,3),dtype=np.uint8)) == 0
    assert helper.input_img.shape == (320,331,3)


@pytest.mark.parametrize('value', [float('nan'), 1.1, -.1])
def test_invalid_detector_score_fails_closed(value):
    row = ROW.copy(); row[4] = value
    with pytest.raises(ValueError, match='CODEFORMER_DETECTOR_GEOMETRY_INVALID'):
        detect(Helper([row]), np.zeros((320,331,3),dtype=np.uint8))


def test_scale_default_is_800_and_every_returned_face_is_preserved():
    # Two separated detector hypotheses are retained; no largest/center/count
    # selector is introduced by scale policy. This fake is NOT detector proof.
    left = ROW.copy(); right = ROW.copy()
    left[0:4] = [.02,.2,.4,.8]
    right[0:4] = [.6,.2,.98,.8]
    helper = Helper([left,right])
    assert detect(helper,np.zeros((1000,1500,3),dtype=np.uint8)) == 2
    assert helper.detection_short_side == 800
    assert helper.detected_shape[:2] == (800,1200)
    assert len(helper.det_faces) == len(helper.all_landmarks_5) == 2
    assert [float(x[4]) for x in helper.det_faces] == pytest.approx([.93,.93])


@pytest.mark.parametrize('resize', [400,800,1600])
def test_explicit_diagnostic_resize_keeps_original_coordinate_mapping(resize):
    image = np.zeros((320,331,3),dtype=np.uint8)
    helper = Helper([ROW])
    assert detect(helper,image,resize=resize) == 1
    np.testing.assert_allclose(helper.det_faces[0], [.1*331,.1*320,.9*331,.9*320,.93],rtol=1e-6)
    assert helper.input_img.shape == image.shape


def test_production_call_uses_default_policy_and_keeps_all_face_loop():
    # Structural source contract guards the only main invocation. Model/gate
    # correctness still requires actual all-input restoration runs.
    calls=[n for n in ast.walk(tree) if isinstance(n,ast.Call) and isinstance(n.func,ast.Name) and n.func.id=='detect_original_geometry']
    assert len(calls)==1
    assert not any(k.arg=='resize' for k in calls[0].keywords)
    main=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='main')
    assert any(isinstance(n,ast.For) and isinstance(n.iter,ast.Call) and isinstance(n.iter.func,ast.Name) and n.iter.func.id=='enumerate' for n in ast.walk(main))
