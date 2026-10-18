import math
import random
from dataclasses import replace
from datetime import datetime

import pytest
from eu_dynamics import CURVES, ProfileDynamics
from eu_support import profile


def test_noise_uses_elapsed_seconds_and_seed_not_number_of_solves(tmp_path):
    settings = replace(profile(tmp_path).profile, seed=42)
    now = [datetime.fromisoformat("2026-10-18T10:00:00+00:00").timestamp()]
    first = ProfileDynamics(settings, lambda: now[0])
    second = ProfileDynamics(settings, lambda: now[0])
    assert first.sample(True) == second.sample(True)
    # Repeated correction solves at the same instant consume no random draws.
    for _ in range(10):
        first.sample(True)
    now[0] += 30
    assert first.sample(True) == second.sample(True)
    rng = random.Random(42)
    expected_noise = (settings.noise_sigma * math.sqrt(
        -math.expm1(-2 * 30 / settings.noise_tau_s)) * rng.gauss(0, 1))
    assert first.noise["residential"] == pytest.approx(expected_noise)
    rng.gauss(0, 1)
    rng.gauss(0, 1)
    expected_source = (1 + settings.noise_sigma * math.sqrt(
        -math.expm1(-2 * 30 / settings.source_tau_s)) * rng.gauss(0, 1))
    assert first.source == pytest.approx(expected_source)
    # A stalled solve advances using the whole gap, not one nominal second.
    now[0] += 300
    assert first.sample(True) == second.sample(True)


def test_daily_curves_and_source_are_bounded_in_local_time(tmp_path):
    settings = replace(profile(tmp_path).profile, seed=7, noise_sigma=0)
    now = [datetime.fromisoformat("2026-10-18T18:00:00+00:00").timestamp()]
    dynamics = ProfileDynamics(settings, lambda: now[0])
    evening = dynamics.sample(True)
    assert evening["load_factors"] == {
        "residential": 1.0, "commercial": .55, "industrial": .9}
    now[0] -= 8 * 3600  # Local noon.
    daytime = dynamics.sample(True)
    assert daytime["load_factors"]["commercial"] == 1
    assert daytime["load_factors"]["residential"] == .6
    noisy = ProfileDynamics(replace(settings, noise_sigma=2), lambda: now[0])
    for _ in range(100):
        now[0] += 100
        sample = noisy.sample(True)
        assert all(.5 <= n <= 1 for n in sample["load_factors"].values())
        assert settings.source_pu_min <= sample["source_pu"] <= settings.source_pu_max


def test_disabled_profile_has_unit_factors_no_noise_and_can_resume(tmp_path):
    now = [1000]
    dynamics = ProfileDynamics(replace(profile(tmp_path).profile, seed=3), lambda: now[0])
    dynamics.sample(True)
    now[0] += 60
    dynamics.sample(True)
    assert any(dynamics.noise.values())
    for _ in range(2):
        now[0] += 10
        assert dynamics.sample(False) == {
            "enabled": False, "load_factors": dict.fromkeys(CURVES, 1.0), "source_pu": 1.0}
        assert not any(dynamics.noise.values())
    assert dynamics.sample(True)["enabled"]


def test_bounding_does_not_modify_latent_ou_statistics(tmp_path):
    settings = replace(profile(tmp_path).profile, seed=7, noise_sigma=2)
    now = [1000]
    dynamics = ProfileDynamics(settings, lambda: now[0])
    dynamics.sample(True)
    rng = random.Random(7)
    latent_source = 1.0
    for _ in range(5):
        now[0] += 100
        sample = dynamics.sample(True)
        for _ in CURVES:
            rng.gauss(0, 1)
        decay = math.exp(-100 / settings.source_tau_s)
        latent_source = (1 + (latent_source - 1) * decay + settings.noise_sigma
                         * math.sqrt(1 - decay * decay) * rng.gauss(0, 1))
        assert dynamics.source == pytest.approx(latent_source)
        assert sample["source_pu"] == max(settings.source_pu_min, min(
            settings.source_pu_max, latent_source))
