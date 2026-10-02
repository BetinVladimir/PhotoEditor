"""Identity alignment must preserve the qualified model's fixed input shape."""

from uuid import uuid4

import numpy as np
import pytest

from photo_worker.operations.identity.embedding import align_identity_face
from photo_worker.operations.session.contracts import (
    FaceObservation, FacePoint, FaceRect,
)


def _face():
    points = [FacePoint(x=.5, y=.5) for _ in range(68)]
    for index in range(36, 42):
        points[index] = FacePoint(x=.4, y=.4)
    for index in range(42, 48):
        points[index] = FacePoint(x=.6, y=.4)
    points[30] = FacePoint(x=.5, y=.525)
    return FaceObservation(observation_id=uuid4(), face_id=uuid4(),
                           box=FaceRect(x0=.25, y0=.25, x1=.75, y1=.75),
                           landmarks=points, embedding=None, quality=1,
                           person_id=None)


def test_identity_alignment_uses_three_pinned_anchors():
    image = np.full((200, 200, 3), .5, np.float32)
    face = _face()
    aligned = align_identity_face(image, face)
    assert aligned.shape == (112, 112, 3)
    assert aligned.dtype == np.float32
    assert np.allclose(aligned, .5)
    with pytest.raises(ValueError, match='IDENTITY_ALIGNMENT_INVALID'):
        align_identity_face(image, face.model_copy(update={
            'landmarks': [FacePoint(x=.5, y=.5) for _ in range(68)]}))
    edge_points = [FacePoint(x=.1, y=.1) for _ in range(68)]
    for index in range(36, 42):
        edge_points[index] = FacePoint(x=.05, y=.1)
    for index in range(42, 48):
        edge_points[index] = FacePoint(x=.15, y=.1)
    edge_points[30] = FacePoint(x=.1, y=.2)
    with pytest.raises(ValueError, match='IDENTITY_ALIGNMENT_OUTSIDE_IMAGE'):
        align_identity_face(image, face.model_copy(update={'landmarks': edge_points}))
