from datetime import datetime, timedelta, timezone

import pytest
from eu_profile import ProtectionSettings
from eu_protection import F2Protection


def stamp(ms):
    return (datetime(2026, 10, 18, tzinfo=timezone.utc) + timedelta(
        milliseconds=ms)).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def relay(delay=1000):
    return F2Protection(ProtectionSettings(400, delay, 3000), 1000)


def test_pickup_and_sample_quantised_trip():
    protection = relay(1500)
    assert not protection.sample(399, stamp(0), "epoch", True)
    assert not protection.sample(400, stamp(1000), "epoch", True)
    assert protection.snapshot() == {
        "picked_up": True, "picked_up_since": stamp(1000), "last_trip_current_a": None}
    assert not protection.sample(450, stamp(2000), "epoch", True)
    assert protection.sample(500, stamp(3000), "epoch", True)
    assert protection.snapshot()["last_trip_current_a"] == 500
    assert not protection.sample(3500, stamp(4000), "epoch", False)
    assert protection.snapshot()["last_trip_current_a"] == 500


@pytest.mark.parametrize("reset_reason", ["dropoff", "epoch", "gap", "invalid", "reset"])
def test_timer_resets_on_dropoff_epoch_gap_invalid_and_generation_reset(reset_reason):
    protection = relay()
    assert not protection.sample(400, stamp(0), "epoch", True)
    epoch, next_ms = "epoch", 1000
    if reset_reason == "dropoff":
        assert not protection.sample(399, stamp(500), epoch, True)
    elif reset_reason == "epoch":
        epoch = "new-process-epoch"
    elif reset_reason == "gap":
        next_ms = 2501
    elif reset_reason == "invalid":
        protection.invalidate()
    else:
        protection.reset()  # F2 process fixture reset, not a field revision.
    assert not protection.sample(400, stamp(next_ms), epoch, True)
    assert protection.snapshot()["picked_up_since"] == stamp(next_ms)
    assert protection.sample(400, stamp(next_ms + 1000), epoch, True)


def test_rejected_replay_neither_advances_nor_resets_and_gap_boundary_is_inclusive():
    protection = relay()
    assert not protection.sample(400, stamp(0), "epoch", True)
    assert not protection.sample(0, stamp(0), "epoch", True)
    assert protection.snapshot()["picked_up_since"] == stamp(0)
    assert protection.sample(400, stamp(2500), "epoch", True)


@pytest.mark.parametrize("current,delay", [(3000, 1000), (400, 0)])
def test_instantaneous_and_zero_delay_act_on_first_sample(current, delay):
    protection = relay(delay)
    assert protection.sample(current, stamp(0), "epoch", True)
    assert protection.snapshot()["last_trip_current_a"] == current
    protection.sample(0, stamp(1), "epoch", False)
    assert not protection.snapshot()["picked_up"]
    assert protection.snapshot()["last_trip_current_a"] == current
    protection.reset()
    assert protection.snapshot()["last_trip_current_a"] is None
