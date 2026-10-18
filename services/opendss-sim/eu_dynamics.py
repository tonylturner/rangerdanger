"""Elapsed-time stochastic teaching profiles; these are DSS inputs, not meters."""

import math
import random
import time
from datetime import datetime
from zoneinfo import ZoneInfo

from eu_profile import ProfileSettings

# Piecewise-linear local-hour anchors; midnight wraps continuously.
CURVES = {
    "residential": ((0, .60), (5, .50), (8, .70), (12, .60), (17, .80),
                    (20, 1.0), (23, .65), (24, .60)),
    "commercial": ((0, .50), (6, .50), (9, .95), (12, 1.0), (17, .95),
                   (20, .55), (24, .50)),
    "industrial": ((0, .90), (24, .90)),
}
LOAD_CATEGORIES = {
    "Residential": "residential", "Commercial": "commercial", "Industrial": "industrial",
}


class ProfileDynamics:
    def __init__(self, settings: ProfileSettings, clock=time.time):
        self.settings = settings
        self.clock = clock
        self.rng = random.Random(settings.seed)
        self.previous = None
        self.noise = dict.fromkeys(CURVES, 0.0)
        self.source = 1.0
        self.zone = ZoneInfo(settings.timezone)

    def _ou(self, value: float, mean: float, dt: float, tau: float) -> float:
        if dt <= 0:
            return value
        decay = math.exp(-dt / tau)
        return (mean + (value - mean) * decay
                + self.settings.noise_sigma * math.sqrt(-math.expm1(-2 * dt / tau))
                * self.rng.gauss(0, 1))

    def sample(self, enabled: bool) -> dict:
        now = self.clock()
        dt = 0 if self.previous is None else max(0, now - self.previous)
        self.previous = now if self.previous is None else max(now, self.previous)
        if not enabled:
            self.noise = dict.fromkeys(CURVES, 0.0)
            self.source = 1.0
            return {"enabled": False, "load_factors": dict.fromkeys(CURVES, 1.0),
                    "source_pu": 1.0}
        local = datetime.fromtimestamp(now, self.zone)
        hour = local.hour + local.minute / 60 + (local.second + local.microsecond / 1e6) / 3600
        factors = {}
        for category, anchors in CURVES.items():
            self.noise[category] = self._ou(
                self.noise[category], 0, dt, self.settings.noise_tau_s)
            for (a, va), (b, vb) in zip(anchors, anchors[1:]):
                if a <= hour <= b:
                    base = va + (vb - va) * (hour - a) / (b - a)
                    break
            factors[category] = max(.5, min(1.0, base + self.noise[category]))
        # Bound the physical input, not the latent OU state: feeding clipped
        # values back would make the noise statistics depend on solve cadence.
        self.source = self._ou(self.source, 1.0, dt, self.settings.source_tau_s)
        source_pu = max(self.settings.source_pu_min, min(self.settings.source_pu_max, self.source))
        return {"enabled": True, "load_factors": factors, "source_pu": source_pu}
