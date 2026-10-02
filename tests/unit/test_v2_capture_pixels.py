import unittest

import numpy as np

from photo_worker.operations.capture.assess import (
    assess_clipping, assess_focus, clipping_score, focus_score,
)


class CapturePixelTest(unittest.TestCase):
    def test_focus_flat_frame_and_textured_frame_are_ordered(self):
        flat = np.full((512, 512, 3), .5, np.float32)
        textured = np.repeat((np.indices((512, 512)).sum(axis=0) % 3 / 2)[..., None], 3, axis=2)
        self.assertEqual(focus_score(flat), 0)
        self.assertGreater(focus_score(textured), .99)
        self.assertEqual(assess_focus(flat, .1, .2).status, 'critical')
        self.assertEqual(assess_focus(flat, 0, .2).status, 'warning')
        self.assertEqual(assess_focus(textured, .1, .2).status, 'pass')

    def test_clipping_exact_endpoints_and_threshold_equality(self):
        rgb = np.array([[[0., 1., .5], [-.1, .5, .5], [.5, .5, 1.1], [.5, .5, .5]]])
        self.assertEqual(clipping_score(rgb), .5)
        self.assertEqual(assess_clipping(rgb, .25, .5).status, 'warning')
        self.assertEqual(assess_clipping(rgb, .1, .4).status, 'critical')
        self.assertEqual(assess_clipping(rgb, .5, .6).status, 'pass')

    def test_nonfinite_or_wrong_canvas_is_rejected(self):
        for source in [np.zeros((2, 2)), np.full((2, 2, 3), np.nan), np.zeros((0, 2, 3))]:
            with self.assertRaises(ValueError):
                focus_score(source)
        with self.assertRaises(ValueError):
            assess_focus(np.zeros((2, 2, 3)), .2, .1)
        with self.assertRaises(ValueError):
            assess_clipping(np.zeros((2, 2, 3)), .5, .5)
