"""Face structure checks supplement a process-installed original producer authority.

An archive, equal canvas, or a self-declared ancestor cannot enable this path.
Authority must authenticate the original detector/model and current-source edge.
"""
from photo_worker.contracts.face import FaceLandmarks, FaceBoxes
from photo_worker.contracts.images import WorkingImage


def qualify_conservative_faces(report, proposal, image, binding, read, authority, active):
    if not callable(authority):
        return False
    if len(report.params.faces) != len(report.faces):
        return False
    report_ids = {face.object_id for face in report.faces}
    if len(report_ids) != len(report.faces):
        return False
    known = set()
    for face in report.params.faces:
        active()
        landmarks = read(face.landmarks, 'FaceLandmarks', FaceLandmarks)
        historical = read(landmarks.image, 'WorkingImage', WorkingImage)
        boxes = read(landmarks.faces, 'FaceBoxes', FaceBoxes)
        if (boxes.image != landmarks.image or historical.canvas != image.canvas
                or historical.transform_chain != image.transform_chain
                or historical.color != image.color or historical.dtype != image.dtype):
            return False
        matching = [value for value in landmarks.landmarks if value.object_id == face.object_id]
        detected = [value for value in boxes.faces if value.object_id == face.object_id]
        if (len(matching) != 1 or len(detected) != 1 or face.object_id not in report_ids
                or matching[0].canvas != image.canvas
                or matching[0].transform_chain != image.transform_chain):
            return False
        if authority(binding.model_copy(deep=True), face.landmarks.model_copy(deep=True),
                     landmarks.model_copy(deep=True), boxes.model_copy(deep=True),
                     historical.model_copy(deep=True)) is not True:
            return False
        known.add(face.object_id)
        active()
    return known == report_ids and sum(face.candidates for face in report.faces) >= len(proposal.candidates)
