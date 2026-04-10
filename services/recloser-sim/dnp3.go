package main

// DNP3 outstation for the recloser simulator.
//
// DNP3 Address: 2, Port: 20000
//
// Point Map:
//
//   Binary Inputs (Group 1 Var 2, read-only):
//     0: lockout
//     1: fault_seen
//     2: auto_mode
//     3: comms_ok
//
//   Binary Outputs (Group 10/12, controllable):
//     0: closed          (CROB: trip/close/latch)
//     1: reclose_enabled (CROB: latch_on=enable, latch_off=disable)
//
//   Analog Inputs (Group 30 Var 5):
//     0: shot_count

import (
	"log"

	"github.com/tonylturner/dnp3go"
	"github.com/tturner/rangerdanger/services/shared"
)

func startDNP3Server() {
	cfg := &dnp3go.OutstationConfig{
		OutstationAddr:  2,
		AcceptAnyMaster: true,

		BinaryInputs: []dnp3go.BinaryPoint{
			{Index: 0, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.Lockout }},
			{Index: 1, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.FaultSeen }},
			{Index: 2, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.AutoMode }},
			{Index: 3, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.CommsOK }},
		},

		BinaryOutputs: []dnp3go.BinaryOutputPoint{
			{
				Index: 0,
				Read: func() bool {
					state.mu.RLock()
					defer state.mu.RUnlock()
					return state.Closed
				},
				Operate: func(controlCode uint8) uint8 {
					state.mu.Lock()
					defer state.mu.Unlock()

					switch controlCode {
					case dnp3go.CROBTripPulse, dnp3go.CROBLatchOff, dnp3go.CROBPulseOff:
						state.Closed = false
						state.LastCommandSource = "dnp3-tcp"
						log.Printf("DNP3 OPEN — recloser opened")
						audit.Add(shared.AuditEntry{
							Source: "dnp3-tcp", Target: "recloser-sim",
							Command: "crob_open", Result: "executed",
							Detail: "recloser OPENED via DNP3 CROB",
						})
						return dnp3go.CROBStatusSuccess

					case dnp3go.CROBClosePulse, dnp3go.CROBLatchOn, dnp3go.CROBPulseOn:
						if state.Lockout {
							log.Printf("DNP3 CLOSE REJECTED: lockout active")
							audit.Add(shared.AuditEntry{
								Source: "dnp3-tcp", Target: "recloser-sim",
								Command: "crob_close", Result: "rejected",
								Detail: "lockout active",
							})
							return dnp3go.CROBStatusBlocked
						}
						state.Closed = true
						state.ShotCount = 0
						state.LastCommandSource = "dnp3-tcp"
						log.Printf("DNP3 CLOSE — recloser closed")
						audit.Add(shared.AuditEntry{
							Source: "dnp3-tcp", Target: "recloser-sim",
							Command: "crob_close", Result: "executed",
							Detail: "recloser CLOSED via DNP3 CROB",
						})
						return dnp3go.CROBStatusSuccess

					default:
						return dnp3go.CROBStatusNotSupported
					}
				},
			},
			{
				Index: 1,
				Read: func() bool {
					state.mu.RLock()
					defer state.mu.RUnlock()
					return state.RecloseEnabled
				},
				Operate: func(controlCode uint8) uint8 {
					state.mu.Lock()
					defer state.mu.Unlock()

					var enabled bool
					switch controlCode {
					case dnp3go.CROBLatchOn, dnp3go.CROBClosePulse, dnp3go.CROBPulseOn:
						enabled = true
					case dnp3go.CROBLatchOff, dnp3go.CROBTripPulse, dnp3go.CROBPulseOff:
						enabled = false
					default:
						return dnp3go.CROBStatusNotSupported
					}

					state.RecloseEnabled = enabled
					state.LastCommandSource = "dnp3-tcp"

					detail := "auto-reclose DISABLED via DNP3 CROB"
					if enabled {
						detail = "auto-reclose ENABLED via DNP3 CROB"
					}
					log.Printf("DNP3 reclose_enabled=%v", enabled)
					audit.Add(shared.AuditEntry{
						Source: "dnp3-tcp", Target: "recloser-sim",
						Command: "crob_reclose", Result: "executed",
						Detail: detail,
					})
					return dnp3go.CROBStatusSuccess
				},
			},
		},

		AnalogInputs: []dnp3go.AnalogPoint{
			{Index: 0, Read: func() float32 { state.mu.RLock(); defer state.mu.RUnlock(); return float32(state.ShotCount) }},
		},
	}

	if err := dnp3go.ListenAndServe(":20000", cfg); err != nil {
		log.Printf("DNP3 server failed: %v", err)
	}
}
