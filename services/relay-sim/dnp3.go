package main

// DNP3 outstation for the relay simulator.
//
// DNP3 Address: 1, Port: 20000
//
// Point Map:
//
//   Binary Inputs (Group 1 Var 2, read-only):
//     0: remote_control_enabled
//     1: lockout
//     2: fault_seen
//     3: comms_ok
//
//   Binary Outputs (Group 10/12, controllable):
//     0: breaker_closed  (CROB: trip_pulse=open, close_pulse=close, latch_on=close, latch_off=open)
//
//   Analog Inputs (Group 30 Var 5):
//     0: measured_current_a
//     1: measured_voltage_kv

import (
	"log"

	"github.com/tonylturner/dnp3go"
	"github.com/tturner/rangerdanger/services/shared"
)

func startDNP3Server() {
	cfg := &dnp3go.OutstationConfig{
		OutstationAddr:  1,
		AcceptAnyMaster: true,

		BinaryInputs: []dnp3go.BinaryPoint{
			{Index: 0, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.RemoteControlEnabled }},
			{Index: 1, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.Lockout }},
			{Index: 2, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.FaultSeen }},
			{Index: 3, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.CommsOK }},
		},

		BinaryOutputs: []dnp3go.BinaryOutputPoint{
			{
				Index: 0,
				Read: func() bool {
					state.mu.RLock()
					defer state.mu.RUnlock()
					return state.BreakerClosed
				},
				Operate: func(controlCode uint8) uint8 {
					state.mu.Lock()
					defer state.mu.Unlock()

					if !state.RemoteControlEnabled {
						log.Printf("DNP3 CROB index 0 REJECTED: remote control disabled")
						audit.Add(shared.AuditEntry{
							Source:  "dnp3-tcp",
							Target:  "relay-sim",
							Command: "crob_breaker",
							Result:  "rejected",
							Detail:  "remote control disabled",
						})
						return dnp3go.CROBStatusBlocked
					}
					if state.Lockout {
						log.Printf("DNP3 CROB index 0 REJECTED: lockout active")
						audit.Add(shared.AuditEntry{
							Source:  "dnp3-tcp",
							Target:  "relay-sim",
							Command: "crob_breaker",
							Result:  "rejected",
							Detail:  "lockout active",
						})
						return dnp3go.CROBStatusBlocked
					}

					var closedVal bool
					var action string
					switch controlCode {
					case dnp3go.CROBTripPulse, dnp3go.CROBLatchOff, dnp3go.CROBPulseOff:
						closedVal = false
						action = "TRIP"
					case dnp3go.CROBClosePulse, dnp3go.CROBLatchOn, dnp3go.CROBPulseOn:
						closedVal = true
						action = "CLOSE"
					default:
						return dnp3go.CROBStatusNotSupported
					}

					state.BreakerClosed = closedVal
					state.LastCommandSource = "dnp3-tcp"
					log.Printf("DNP3 %s — breaker_closed=%v", action, closedVal)

					audit.Add(shared.AuditEntry{
						Source:  "dnp3-tcp",
						Target:  "relay-sim",
						Command: "crob_breaker",
						Result:  "executed",
						Detail:  "breaker " + action + " via DNP3 CROB",
					})
					return dnp3go.CROBStatusSuccess
				},
			},
		},

		AnalogInputs: []dnp3go.AnalogPoint{
			{Index: 0, Read: func() float32 { state.mu.RLock(); defer state.mu.RUnlock(); return float32(state.MeasuredCurrent) }},
			{Index: 1, Read: func() float32 { state.mu.RLock(); defer state.mu.RUnlock(); return float32(state.MeasuredVoltage) }},
		},
	}

	if err := dnp3go.ListenAndServe(":20000", cfg); err != nil {
		log.Printf("DNP3 server failed: %v", err)
	}
}
