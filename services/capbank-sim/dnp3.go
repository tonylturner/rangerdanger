package main

// DNP3 outstation for the switched capacitor bank simulator.
//
// DNP3 Address: 4, Port: 20000
//
// Point Map:
//
//   Binary Inputs (Group 1 Var 2, read-only):
//     0: switched_in
//     1: auto_mode
//     2: lockout
//     3: alarm
//     4: comms_ok
//
//   Binary Outputs (Group 10/12, controllable):
//     0: switched_in   (latch_on=switch_in, latch_off=switch_out)
//     1: auto_mode     (latch_on=auto, latch_off=manual)
//     2: lockout_reset (any operate = reset lockout)
//
//   Analog Inputs (Group 30 Var 5, read-only):
//     0: kvar_rating
//     1: switch_count
//
//   No analog outputs (thresholds are set via HTTP only).

import (
	"log"

	"github.com/tonylturner/dnp3go"
	"github.com/tturner/rangerdanger/services/shared"
)

func startDNP3Server() {
	cfg := &dnp3go.OutstationConfig{
		OutstationAddr:  4,
		AcceptAnyMaster: true,

		BinaryInputs: []dnp3go.BinaryPoint{
			{Index: 0, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.SwitchedIn }},
			{Index: 1, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.AutoMode }},
			{Index: 2, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.Lockout }},
			{Index: 3, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.Alarm }},
			{Index: 4, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.CommsOK }},
		},

		BinaryOutputs: []dnp3go.BinaryOutputPoint{
			{
				// Index 0: switch_in / switch_out
				Index: 0,
				Read: func() bool {
					state.mu.RLock()
					defer state.mu.RUnlock()
					return state.SwitchedIn
				},
				Operate: func(controlCode uint8) uint8 {
					state.mu.Lock()
					defer state.mu.Unlock()

					if state.Lockout {
						log.Println("DNP3 CROB switch rejected — lockout active")
						return dnp3go.CROBStatusBlocked
					}

					var switchIn bool
					switch controlCode {
					case dnp3go.CROBLatchOn, dnp3go.CROBClosePulse, dnp3go.CROBPulseOn:
						switchIn = true
					case dnp3go.CROBLatchOff, dnp3go.CROBTripPulse, dnp3go.CROBPulseOff:
						switchIn = false
					default:
						return dnp3go.CROBStatusNotSupported
					}

					if switchIn == state.SwitchedIn {
						return dnp3go.CROBStatusSuccess // no-op
					}

					state.SwitchedIn = switchIn
					state.SwitchCount++
					state.LastCommandSource = "dnp3-tcp"
					if state.SwitchCount >= maxSwitchCount {
						state.Lockout = true
						state.Alarm = true
					}

					action := "switch_out"
					if switchIn {
						action = "switch_in"
					}
					log.Printf("DNP3 CROB %s (count: %d)", action, state.SwitchCount)
					audit.Add(shared.AuditEntry{
						Source: "dnp3-tcp", Target: "capbank-sim",
						Command: action, Result: "executed",
						Detail: "via DNP3 CROB",
					})
					return dnp3go.CROBStatusSuccess
				},
			},
			{
				// Index 1: auto_mode on/off
				Index: 1,
				Read: func() bool {
					state.mu.RLock()
					defer state.mu.RUnlock()
					return state.AutoMode
				},
				Operate: func(controlCode uint8) uint8 {
					state.mu.Lock()
					defer state.mu.Unlock()

					var autoOn bool
					switch controlCode {
					case dnp3go.CROBLatchOn, dnp3go.CROBClosePulse, dnp3go.CROBPulseOn:
						autoOn = true
					case dnp3go.CROBLatchOff, dnp3go.CROBTripPulse, dnp3go.CROBPulseOff:
						autoOn = false
					default:
						return dnp3go.CROBStatusNotSupported
					}

					state.AutoMode = autoOn
					state.LastCommandSource = "dnp3-tcp"

					mode := "manual"
					if autoOn {
						mode = "auto"
					}
					log.Printf("DNP3 CROB auto_mode=%s", mode)
					audit.Add(shared.AuditEntry{
						Source: "dnp3-tcp", Target: "capbank-sim",
						Command: "set_" + mode, Result: "executed",
						Detail: "via DNP3 CROB",
					})
					return dnp3go.CROBStatusSuccess
				},
			},
			{
				// Index 2: lockout reset (any operate = reset)
				Index: 2,
				Read: func() bool {
					state.mu.RLock()
					defer state.mu.RUnlock()
					return state.Lockout
				},
				Operate: func(_ uint8) uint8 {
					state.mu.Lock()
					defer state.mu.Unlock()

					state.Lockout = false
					state.Alarm = false
					state.SwitchCount = 0
					state.LastCommandSource = "dnp3-tcp"

					log.Println("DNP3 CROB lockout_reset")
					audit.Add(shared.AuditEntry{
						Source: "dnp3-tcp", Target: "capbank-sim",
						Command: "reset_lockout", Result: "executed",
						Detail: "via DNP3 CROB",
					})
					return dnp3go.CROBStatusSuccess
				},
			},
		},

		AnalogInputs: []dnp3go.AnalogPoint{
			{Index: 0, Read: func() float32 {
				state.mu.RLock()
				defer state.mu.RUnlock()
				return float32(state.KvarRating)
			}},
			{Index: 1, Read: func() float32 {
				state.mu.RLock()
				defer state.mu.RUnlock()
				return float32(state.SwitchCount)
			}},
		},
	}

	if err := dnp3go.ListenAndServe(":20000", cfg); err != nil {
		log.Printf("DNP3 server failed: %v", err)
	}
}
