"""Run the existing whole SQL settlement family in an owned migrated database.
Physical purge remains the maintenance caller's precondition; this adapter does
not replace object-store verification or create a successful purge receipt.
"""
import psycopg
import pytest
from conftest import database as isolated_database
from test_output_cleanup_reservation_refund import *  # noqa: F403

@pytest.fixture
def database(isolated_database):
    with psycopg.connect(**isolated_database) as connection:
        try:
            yield connection
        finally:
            connection.rollback()
