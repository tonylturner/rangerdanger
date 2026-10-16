package server

func evaluateEULab3(ev euLab3Evidence, policyOK bool) []ValidationCheck {
	if ev.err != nil {
		return []ValidationCheck{checkFail("RMU evidence available", ev.err.Error())}
	}
	return []ValidationCheck{
		euLab3Check("RMU1 CC switching", ev.commandAudit("rmu-1", "rmu-1-command", "open", "completed", "", 1101) &&
			ev.commandAudit("rmu-1", "rmu-1-command", "close", "completed", "", 1101),
			"CC open and close with ACT-CON, fresh matching feedback, ACT-TERM and corresponding device writes"),
		euLab3Check("Local authority refusal", ev.commandAudit("rmu-1", "rmu-1-command", "open", "rejected", "not_remote", 1101),
			"negative ACT-CON and not_remote audit during the same execute window"),
		euLab3Check("Live-live interlock refusal", ev.commandAudit("rmu-2", "rmu-2-command", "close", "rejected", "live_live", 1201),
			"negative ACT-CON and live_live audit during the same execute window"),
		euLab3Check("Trip, isolation and back-feed", euLab3Restoration(ev),
			"local protection trip, subsequent RMU1 open and RMU2 close with completed IEC 104 feedback"),
		euLab3Check("Vendor operation and later source deny", policyOK && ev.vendorPair("rmu-2", "10.40.40.22", "open"),
			"vendor Modbus open followed by a same-source TCP/502 deny; running policy matches improved (payload of denied SYN is not observable)"),
	}
}

func euLab3Restoration(ev euLab3Evidence) bool {
	breaker := ev.audits["breaker-1"]
	if breaker == nil {
		return false
	}
	for _, trip := range breaker.Entries {
		if trip.Operation != "local_protection_trip" || trip.Result != "accepted" ||
			trip.Context.Protocol != "local-protection" || !insideEvidenceWindow(trip.At, ev.start, ev.end) {
			continue
		}
		// Restrict both command windows to this trip, and require isolation
		// to complete before the tie's select starts.
		for _, open := range ev.commands() {
			if open.Point != "rmu-1-command" || open.Operation != "open" ||
				open.Result != "completed" || !open.SelectedAt.After(trip.At) {
				continue
			}
			isolation := ev.audits["rmu-1"]
			if isolation == nil || !ev.matchesCommand(isolation, open, "", 1101) {
				continue
			}
			backfeed := ev
			backfeed.start = open.TermAt
			if backfeed.commandAudit("rmu-2", "rmu-2-command", "close", "completed", "", 1201) {
				return true
			}
		}
	}
	return false
}
