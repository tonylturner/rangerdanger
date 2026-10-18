"""Sample-quantised process-owned F2 overcurrent relay."""

from datetime import datetime

from eu_profile import ProtectionSettings


class F2Protection:
    def __init__(self, settings: ProtectionSettings, cadence_ms: int):
        self.settings = settings
        self.max_gap_s = 2.5 * cadence_ms / 1000
        self.epoch = None
        self.previous = None
        self.picked_up_since = None
        self.last_trip_current_a = None

    def invalidate(self) -> None:
        self.previous = None
        self.picked_up_since = None

    def reset(self) -> None:
        self.invalidate()
        self.last_trip_current_a = None

    def snapshot(self) -> dict:
        return {"picked_up": self.picked_up_since is not None,
                "picked_up_since": self.picked_up_since,
                "last_trip_current_a": self.last_trip_current_a}

    def sample(self, current: float, solved_at: str, epoch: str, closed: bool) -> bool:
        now = datetime.fromisoformat(solved_at.replace("Z", "+00:00"))
        if self.epoch != epoch:
            self.invalidate()
            self.epoch = epoch
        if self.previous is not None:
            gap = (now - self.previous).total_seconds()
            if gap <= 0:
                return False  # A rejected sample neither advances nor resets.
            if gap > self.max_gap_s:
                self.invalidate()
        self.previous = now
        if current < self.settings.pickup_a:
            self.picked_up_since = None
            return False
        if self.picked_up_since is None:
            self.picked_up_since = solved_at
        elapsed_ms = (now - datetime.fromisoformat(
            self.picked_up_since.replace("Z", "+00:00"))).total_seconds() * 1000
        if closed and (current >= self.settings.instantaneous_a
                       or elapsed_ms >= self.settings.delay_ms):
            self.last_trip_current_a = current
            return True
        return False
