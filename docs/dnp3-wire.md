# DNP3 wire behavior

The range's `dnp3go` implements a bounded subset of IEEE Std 1815 for the
simulators and operator tools, not a certified complete DNP3 stack.

## Reading captures and command output

- Outstation user-data responses have link control `0x44`: DIR=0, PRM=1,
  function 4 (Unconfirmed User Data). Application responses use FC `0x81`.
- Group 41 Variation 3 command objects carry a little-endian float32 **then**
  the command status. For tap `-16` and success, the object is
  `00 00 80 c1 00`. This differs from Group 40 Variation 3 *status* objects,
  whose quality flags precede the float.
- CROB responses echo control, count, on-time and off-time, replacing only
  the final result status. `dnp3cmd` uses count 1 and 1000 ms on/off times.
- An accepted `dnp3cmd ... analog 0 -16` prints `Result: SUCCESS`, including
  in `-sbo` mode. Device interlocks can still return a non-success status.
- Unsupported application functions set IIN2.0 (Function Code Not Supported),
  not IIN1.0 (Broadcast).

## Restart and read-only observers

IIN1.7 (Device Restart) stays asserted across polls and TCP reconnections
until a master explicitly clears it. In `dnp3poll` output it appears as
`IIN: 0x8000` if no other indication is set. This is informational, not a
failed poll. `dnp3cmd` excludes that bit when checking for outstation errors,
including on both replies in an SBO exchange.

The RTAC's DNP3 path is a protocol-visibility observer; HTTP remains its
primary data source. It intentionally sends only Class 0 READs, logs IIN,
and does not implement the startup synchronization sequence of an operational
SCADA master. Likewise, `dnp3poll` is a read-only diagnostic tool, not a
restart-acknowledgment tool. Neither silently adds WRITE traffic to a poll.
The existing FC01 G60V1 qualifier `0x06` request bytes are unchanged.

A supervising master can send FC02 with G80V1, range 7..7, packed value
false: application bytes `c0 02 50 01 00 07 07 00` (sequence 0). The
clearing response and subsequent polls no longer assert restart. Only this
IIN write is supported; malformed writes and other IIN indices cannot clear it.
Class 1/2/3 polls return no objects, because the simulator has no event buffer;
Class 0 continues to return static points.

## Select matching and size limits

OPERATE requires a matching SELECT on the same connection within five
seconds. Index, group and all object bytes must match, including CROB count
and times and the analog float's exact bits. Missing, expired or different
selections return **NO_SELECT (2)** and do not actuate the point. This uses
the IEEE Std 1815 application-layer command-status definition of NO_SELECT:
there is no matching selection for the received OPERATE. A new SELECT
replaces the pending set; OPERATE consumes it, including unsuccessful attempts.

The link sender rejects payloads over 250 octets with `ErrFrameTooLarge`
without emitting bytes. It does not perform outgoing transport segmentation.
An oversized outstation response is logged and the connection closed; callers
see a read error rather than corrupt data. Current simulator profiles fit.
For a response containing only G30V5 analog inputs, 48 points fit exactly;
49 points exceed the limit.

The protocol rules above are interpreted from the IEEE 1815 object layouts,
internal-indication and command-status definitions, with packet decoding
checked independently using tshark. That bounded check does not establish
external-master acceptance or full IEEE 1815 conformance.
