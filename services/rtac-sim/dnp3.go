package main

// DNP3 outstation for the RTAC aggregator (read-only).
//
// DNP3 Address: 10, Port: 20000
//
// Point Map (mirrors Modbus register map):
//
//   Binary Inputs (Group 1 Var 2, read-only):
//     0:  breaker_closed
//     1:  recloser_closed
//     2:  general_load_energized
//     3:  critical_load_energized
//     4:  reclose_enabled
//     5:  remote_control_enabled
//     6:  regulator_auto_mode (inverted manual_mode)
//     7:  relay_comms_ok
//     8:  recloser_comms_ok
//     9:  regulator_comms_ok
//     10: relay_fault
//     11: relay_lockout
//     12: recloser_fault
//     13: recloser_lockout
//     14: regulator_alarm
//
//   Analog Inputs (Group 30 Var 5, read-only):
//     0: substation_bus_voltage_v
//     1: downstream_voltage_v
//     2: critical_load_voltage_v
//     3: feeder_current_a
//     4: general_load_kw
//     5: critical_load_kw
//     6: regulator_tap_position
//     7: recloser_shot_count

import (
	"log"

	"github.com/tonylturner/dnp3go"
)

// helper to read a bool from aggregated device state
func aggDeviceBool(device, key string) bool {
	agg.mu.RLock()
	defer agg.mu.RUnlock()
	if dev, ok := agg.Devices[device]; ok {
		v, _ := dev[key].(bool)
		return v
	}
	return false
}

// helper to read a float from aggregated electrical state
func aggElecFloat(key string) float32 {
	agg.mu.RLock()
	defer agg.mu.RUnlock()
	if v, ok := agg.Electrical[key]; ok {
		switch f := v.(type) {
		case float64:
			return float32(f)
		case float32:
			return f
		case int:
			return float32(f)
		}
	}
	return 0
}

func aggComms(device string) bool {
	agg.mu.RLock()
	defer agg.mu.RUnlock()
	return agg.DeviceComms[device]
}

func aggDeviceFloat(device, key string) float32 {
	agg.mu.RLock()
	defer agg.mu.RUnlock()
	if dev, ok := agg.Devices[device]; ok {
		switch f := dev[key].(type) {
		case float64:
			return float32(f)
		case float32:
			return f
		case int:
			return float32(f)
		}
	}
	return 0
}

func startDNP3Server() {
	cfg := &dnp3go.OutstationConfig{
		OutstationAddr:  10,
		AcceptAnyMaster: true,

		BinaryInputs: []dnp3go.BinaryPoint{
			{Index: 0, Read: func() bool { return aggDeviceBool("relay", "breaker_closed") }},
			{Index: 1, Read: func() bool { return aggDeviceBool("recloser", "closed") }},
			{Index: 2, Read: func() bool {
				agg.mu.RLock()
				defer agg.mu.RUnlock()
				v, _ := agg.Electrical["general_load_energized"].(bool)
				return v
			}},
			{Index: 3, Read: func() bool {
				agg.mu.RLock()
				defer agg.mu.RUnlock()
				v, _ := agg.Electrical["critical_load_energized"].(bool)
				return v
			}},
			{Index: 4, Read: func() bool { return aggDeviceBool("recloser", "reclose_enabled") }},
			{Index: 5, Read: func() bool { return aggDeviceBool("relay", "remote_control_enabled") }},
			{Index: 6, Read: func() bool { return !aggDeviceBool("regulator", "manual_mode") }}, // auto = !manual
			{Index: 7, Read: func() bool { return aggComms("relay") }},
			{Index: 8, Read: func() bool { return aggComms("recloser") }},
			{Index: 9, Read: func() bool { return aggComms("regulator") }},
			{Index: 10, Read: func() bool { return aggDeviceBool("relay", "fault_seen") }},
			{Index: 11, Read: func() bool { return aggDeviceBool("relay", "lockout") }},
			{Index: 12, Read: func() bool { return aggDeviceBool("recloser", "fault_seen") }},
			{Index: 13, Read: func() bool { return aggDeviceBool("recloser", "lockout") }},
			{Index: 14, Read: func() bool { return aggDeviceBool("regulator", "alarm") }},
		},

		AnalogInputs: []dnp3go.AnalogPoint{
			{Index: 0, Read: func() float32 { return aggElecFloat("substation_bus_voltage_v") }},
			{Index: 1, Read: func() float32 { return aggElecFloat("downstream_voltage_v") }},
			{Index: 2, Read: func() float32 { return aggElecFloat("critical_load_voltage_v") }},
			{Index: 3, Read: func() float32 { return aggElecFloat("feeder_current_a") }},
			{Index: 4, Read: func() float32 { return aggElecFloat("general_load_kw") }},
			{Index: 5, Read: func() float32 { return aggElecFloat("critical_load_kw") }},
			{Index: 6, Read: func() float32 { return aggDeviceFloat("regulator", "tap_position") }},
			{Index: 7, Read: func() float32 { return aggDeviceFloat("recloser", "shot_count") }},
		},

		// No binary or analog outputs — RTAC is read-only aggregator
	}

	if err := dnp3go.ListenAndServe(":20000", cfg); err != nil {
		log.Printf("DNP3 server failed: %v", err)
	}
}
