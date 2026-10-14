from uuid import uuid4
import pytest

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.adoption_service import _publish_donor_review


class ReviewConnection:
    def __init__(self, value):
        self.value = value
        self.calls = []

    def execute(self, sql, params):
        self.calls.append((sql, params))
        return self

    def fetchone(self):
        return self.value


def test_donor_review_uses_exact_adopted_task_result():
    tenant, task, result, review = uuid4(), uuid4(), uuid4(), uuid4()
    connection = ReviewConnection((review,))
    assert _publish_donor_review(connection, tenant, task, result) == review
    assert connection.calls == [(
        'SELECT photo.create_v28_open_eyes_review(%s,%s,%s)',
        (tenant, task, result),
    )]


@pytest.mark.parametrize('response', [None, (None,), ('not-a-uuid',)])
def test_donor_review_failure_prevents_adoption_commit(response):
    connection = ReviewConnection(response)
    with pytest.raises(AdoptionInvalid, match='DONOR_REVIEW_PUBLICATION_STALE'):
        _publish_donor_review(connection, uuid4(), uuid4(), uuid4())
