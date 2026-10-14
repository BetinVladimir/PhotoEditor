"""One task-wide byte ledger shared by import and recipe qualification."""
from .adoption import AdoptionInvalid


class ImportReadBudget:
    def __init__(self, max_json_bytes):
        if type(max_json_bytes) is not int or not 1 <= max_json_bytes <= 128 << 20:
            raise ValueError('INTERCHANGE_READ_BUDGET_INSTALLATION')
        self.maximum = max_json_bytes
        self._json_bytes = self._input_bytes = 0

    @property
    def remaining_json_bytes(self):
        return self.maximum - self._json_bytes

    @property
    def remaining_input_bytes(self):
        return (128 << 30) - self._input_bytes

    @property
    def input_bytes(self):
        return self._input_bytes

    def consume(self, size, *, is_json):
        if type(size) is not int or size < 0 or type(is_json) is not bool:
            raise AdoptionInvalid('INTERCHANGE_READ_BUDGET')
        if (self._input_bytes + size > 128 << 30
                or is_json and size > self.remaining_json_bytes):
            raise AdoptionInvalid('INTERCHANGE_READ_BUDGET')
        self._input_bytes += size
        if is_json:
            self._json_bytes += size
