package main

// DNP3 outstation for the voltage regulator simulator.
//
// DNP3 Address: 3, Port: 20000
//
// Point Map:
//
//   Binary Inputs (Group 1 Var 2, read-only):
//     0: alarm
//     1: comms_ok
//
//   Binary Outputs (Group 10/12, controllable):
//     0: manual_mode  (latch_on=manual, latch_off=auto)
//
//   Analog Inputs (Group 30 Var 5):
//     0: voltage_setpoint
//     1: voltage_offset (tap * 0.75)
//
//   Analog Outputs (Group 40/41):
//     0: tap_position (float32, cast to int, clamped -16..+16)

import (
	"fmt"
	"log"

	"github.com/tonylturner/dnp3go"
	"github.com/tturner/rangerdanger/services/shared"
)

func startDNP3Server() {
	cfg := &dnp3go.OutstationConfig{
		OutstationAddr:  3,
		AcceptAnyMaster: true,

		BinaryInputs: []dnp3go.BinaryPoint{
			{Index: 0, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.Alarm }},
			{Index: 1, Read: func() bool { state.mu.RLock(); defer state.mu.RUnlock(); return state.CommsOK }},
		},

		BinaryOutputs: []dnp3go.BinaryOutputPoint{
			{
				Index: 0,
				Read: func() bool {
					state.mu.RLock()
					defer state.mu.RUnlock()
					return state.ManualMode
				},
				Operate: func(controlCode uint8) uint8 {
					state.mu.Lock()
					defer state.mu.Unlock()

					var manualOn bool
					switch controlCode {
					case dnp3go.CROBLatchOn, dnp3go.CROBClosePulse, dnp3go.CROBPulseOn:
						manualOn = true
					case dnp3go.CROBLatchOff, dnp3go.CROBTripPulse, dnp3go.CROBPulseOff:
						manualOn = false
					default:
						return dnp3go.CROBStatusNotSupported
					}

					state.ManualMode = manualOn
					state.LastCommandSource = "dnp3-tcp"

					detail := "auto mode enabled via DNP3 CROB"
					if manualOn {
						detail = "manual mode enabled via DNP3 CROB"
					}
					log.Printf("DNP3 manual_mode=%v", manualOn)
					audit.Add(shared.AuditEntry{
						Source: "dnp3-tcp", Target: "regulator-sim",
						Command: "crob_manual_mode", Result: "executed",
						Detail: detail,
					})
					return dnp3go.CROBStatusSuccess
				},
			},
		},

		AnalogInputs: []dnp3go.AnalogPoint{
			{Index: 0, Read: func() float32 { state.mu.RLock(); defer state.mu.RUnlock(); return float32(state.VoltageSetpoint) }},
			{Index: 1, Read: func() float32 {
				state.mu.RLock()
				defer state.mu.RUnlock()
				return float32(float64(state.TapPosition) * voltsPerTap)
			}},
		},

		AnalogOutputs: []dnp3go.AnalogOutputPoint{
			{
				Index: 0,
				Read: func() float32 {
					state.mu.RLock()
					defer state.mu.RUnlock()
					return float32(state.TapPosition)
				},
				Operate: func(value float32) uint8 {
					tap := int(value)
					if tap < minTap || tap > maxTap {
						log.Printf("DNP3 analog output: tap %d out of range [%d..%d]", tap, minTap, maxTap)
						return 0x04 // parameter error
					}

					state.mu.Lock()
					defer state.mu.Unlock()

					state.TapPosition = tap
					state.LastCommandSource = "dnp3-tcp"
					estimatedV := 117.6 + float64(tap)*voltsPerTap
					state.Alarm = estimatedV < 108.0 || estimatedV > 132.0

					detail := fmt.Sprintf("tap set to %d via DNP3 (est. %.1fV)", tap, estimatedV)
					if state.Alarm {
						detail = fmt.Sprintf("tap set to %d via DNP3 — VOLTAGE ALARM: est. %.1fV", tap, estimatedV)
					}
					log.Printf("DNP3 tap=%d est=%.1fV alarm=%v", tap, estimatedV, state.Alarm)

					audit.Add(shared.AuditEntry{
						Source: "dnp3-tcp", Target: "regulator-sim",
						Command: "analog_output_tap", Result: "executed",
						Detail: detail,
					})
					return 0 // success
				},
			},
		},
	}

	if err := dnp3go.ListenAndServe(":20000", cfg); err != nil {
		log.Printf("DNP3 server failed: %v", err)
	}
}
