"""Shared time formatting helpers for OpenDSS runtime state."""

from datetime import datetime, timezone


def utc_timestamp() -> str:
    """Return a UTC timestamp at the API's millisecond precision."""
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace(
        "+00:00", "Z"
    )
