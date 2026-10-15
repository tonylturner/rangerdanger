"""Process-owned fault, source and load controls; no field-device authority."""

import math
from dataclasses import asdict, dataclass


class ActionError(ValueError):
    """An action body is not in the frozen process vocabulary."""


@dataclass
class ProcessControls:
    fault_section: str = "none"
    f2_tripped: bool = False
    load_multiplier: float = 1.0
    source_pu: float = 1.0

    def snapshot(self) -> dict:
        return asdict(self)

    def action(self, body: object) -> None:
        if not isinstance(body, dict) or not isinstance(body.get("action"), str):
            raise ActionError("invalid_action")
        action = body["action"]
        ranges = {"set_load": (0.2, 3.0), "set_source": (0.90, 1.10)}
        if action in ranges:
            value = body.get("value")
            low, high = ranges[action]
            if (set(body) != {"action", "value"} or type(value) not in (int, float)
                    or not low <= value <= high or not math.isfinite(value)):
                raise ActionError("invalid_value")
            if action == "set_load":
                self.load_multiplier = float(value)
            else:
                self.source_pu = float(value)
        else:
            if set(body) != {"action"}:
                raise ActionError("invalid_action")
            if action in ("fault_s1", "fault_s2"):
                self.fault_section = action[-2:].upper()
            elif action == "clear_fault":
                self.fault_section = "none"
            elif action == "reset_f2":
                self.f2_tripped = False
            elif action == "reset":
                self.fault_section = "none"
                self.f2_tripped = False
                self.load_multiplier = self.source_pu = 1.0
            else:
                raise ActionError("unknown_action")
