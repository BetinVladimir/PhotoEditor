"""Graph seal/replay against the full production migration sequence."""
from test_v2_task_input_registration import _exercise_task_input_registration


def test_full_migration_graph_seal_and_replay(db):
    _exercise_task_input_registration(db)
