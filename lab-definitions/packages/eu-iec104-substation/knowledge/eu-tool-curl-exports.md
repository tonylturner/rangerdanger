Use HTTP exports to preserve what each part of the range actually knew. The operator endpoint reports CC observations, field state reports devices and the electrical solve, and the historian reports archived CC snapshots. They are complementary evidence sources, not interchangeable ways to obtain a nicer-looking value.

### Export through the host portal

Run these in a host terminal with `curl` available, while the EU range is active. Port 8088 is bound to host loopback; `localhost` inside Kali would mean Kali itself.

```sh
curl --fail --silent --show-error --max-time 10 \
  http://localhost:8088/api/operator/state -o cc-before.json
curl --fail --silent --show-error --max-time 10 \
  'http://localhost:8088/api/operator/journal?after=0' -o cc-journal-before.json
curl --fail --silent --show-error --max-time 10 \
  http://localhost:8088/api/field/state -o field-before.json
curl --fail --silent --show-error --max-time 10 \
  http://localhost:8088/api/historian/points -o historian-points.json
```

Use a dedicated evidence directory and distinct before/after filenames. `--fail` makes an HTTP error fail the command; a saved filename alone does not prove success. Preserve the exit result and do not treat an old file from an earlier attempt as a new response.

The public operator routes are `/api/operator/*`, not `/api/iec104/*`. The CC's own `/api/state` is an upstream management endpoint; the backend adds package/range identity to the operator state.

### Bound the attempt window

Inspect `cache_epoch`, `snapshot_seq`, `rendered_at`, link/GI state (`trigger`, `last_complete_at`, `next_due_at`), and each point’s `kind`, received `asdu`, `reporting`, quality, `received_at` and `stale_reason`. Quiet status receipt age is not an expiry rule; floats expose NT/`not_updated` after three cyclic periods. Record the journal's `latest_seq` before the exercise. Afterwards, request `after=<that-number>` and retain the new oldest/latest bounds. Replace the number explicitly; do not assume every run starts at sequence 1.

An epoch change or an overwritten journal interval invalidates a simple before/after chronology. More snapshots do not guarantee fresh telemetry: rendering state increments snapshot sequence even when no new point arrived.

Field state contains `devices` keyed by node id and a separate `process`. It fails with 502 if an upstream fails. Do not fill that failure from an older snapshot or copy process truth into the operator record.

### Export a historian interval

In the same host shell, set `SINCE` and `UNTIL` to the actual UTC RFC 3339 bounds of your attempt, then run:

```sh
curl --fail --silent --show-error --max-time 10 --get \
  http://localhost:8088/api/historian/history \
  --data-urlencode 'point=oltc-1-bus-kv' \
  --data-urlencode "since=$SINCE" --data-urlencode "until=$UNTIL" \
  -o historian-window.json
```

All three query parameters are required exactly once. Preserve segments, gaps, quality and timestamps; do not flatten missing data to zero. Historian snapshots are not a complete sequence-of-events archive.

To test the enterprise read conduit rather than the management proxy, run from Kali, whose image explicitly installs curl:

```sh
curl --fail --silent --show-error --max-time 5 \
  http://10.70.70.40:8080/api/points
```

The host proxy's success does not prove this routed path works. Ingest is separate TCP/8081 and is not exposed by the backend historian read proxy.

### State the audit limitation

`GET /api/field/audit?after=0` currently proxies only the breaker's audit, not a multi-device audit collection. Do not label it RMU or OLTC evidence. Use device evidence supplied by the corresponding lab and name its provenance. Keep [command results](#eu-control-authority), observation state and physical consequences separate in the final timeline.
