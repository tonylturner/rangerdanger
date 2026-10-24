// SPDX-License-Identifier: Apache-2.0

// Package pointmap loads the stations' shared, strict schema-2 configuration.
package pointmap

// Table is the Modbus register table.
type Table string

const (
	Input   Table = "input"
	Holding Table = "holding"
)

// Kind is the application point or command kind.
type Kind string

const (
	DoublePoint    Kind = "double-point"
	SinglePoint    Kind = "single-point"
	Float          Kind = "float"
	StepPosition   Kind = "step-position"
	DoubleCommand  Kind = "double-command"
	RegulatingStep Kind = "regulating-step"
	SingleCommand  Kind = "single-command"
)

// Format describes the register representation.
type Format string

const (
	DP   Format = "dp"
	Bit  Format = "bit"
	U16  Format = "u16"
	I16  Format = "i16"
	Step Format = "step"
	F32  Format = "f32"
)

// Timers contains APCI settings in seconds and frame counts.
type Timers struct{ T0, T1, T2, T3, K, W int }

// Clock configures the HTTP GPS clock source.
type Clock struct {
	URL                  string
	PollMS, StaleAfterMS int
	SyncedStatus         []string
}

// StatusSynced tests exact status membership.
func (c Clock) StatusSynced(s string) bool {
	for _, status := range c.SyncedStatus {
		if status == s {
			return true
		}
	}
	return false
}

// Device describes one independently polled Modbus device.
type Device struct {
	ID, Host                                    string
	Port, Unit, PollMS, TimeoutMS, StaleAfterMS int
	PollTable                                   Table
	PollStart, PollCount                        int
	HasHeartbeat                                bool
	HeartbeatAddress, HeartbeatStaleMS          int
	HasMeasurementsValid                        bool
	MVTable                                     Table
	MVAddress, MVBit                            int
}

// Point is a resolved monitoring point, in configuration order.
type Point struct {
	ID, ASDU, Unit                 string
	IOA                            int
	Kind                           Kind
	Measurement                    bool
	CyclicMS                       int
	Deadband                       float64
	HasDeadband, HasLimits         bool
	LimitLow, LimitHigh            float64
	DeviceIndex                    int
	Table                          Table
	Address                        int
	Format                         Format
	Bit                            int
	Scale                          float64
	TransientAddress, TransientBit int
}

// Command is a resolved SBO command.
type Command struct {
	ID, ASDU, FeedbackID       string
	IOA                        int
	Kind                       Kind
	SBO                        bool
	DeviceIndex, FeedbackIndex int
	Table                      Table
	Address                    int
}

// Map is a validated station configuration. Treat it as immutable after load.
type Map struct {
	Schema, CA                                         int
	Timers                                             Timers
	SelectTimeoutMS, ReconnectIntervalMS, GIIntervalMS int
	Clock                                              Clock
	Devices                                            []Device
	Points                                             []Point
	Commands                                           []Command
}

// PointByIOA returns a configured point or nil.
func (m *Map) PointByIOA(ioa int) *Point {
	for i := range m.Points {
		if m.Points[i].IOA == ioa {
			return &m.Points[i]
		}
	}
	return nil
}

// CommandByIOA returns a configured command or nil.
func (m *Map) CommandByIOA(ioa int) *Command {
	for i := range m.Commands {
		if m.Commands[i].IOA == ioa {
			return &m.Commands[i]
		}
	}
	return nil
}
