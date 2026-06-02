// dnp3cmd — send DNP3 Direct Operate commands to an outstation.
//
// Usage:
//
//	dnp3cmd <host:port> -a <addr> crob <index> <trip|close|latch-on|latch-off>
//	dnp3cmd <host:port> -a <addr> analog <index> <value>
//
// Examples:
//
//	dnp3cmd 10.40.40.20:20000 -a 1 crob 0 trip          # Trip breaker
//	dnp3cmd 10.40.40.20:20000 -a 1 crob 0 close         # Close breaker
//	dnp3cmd 10.40.40.21:20000 -a 2 crob 1 latch-off     # Disable auto-reclose
//	dnp3cmd 10.40.40.21:20000 -a 2 crob 1 latch-on      # Enable auto-reclose
//	dnp3cmd 10.40.40.22:20000 -a 3 analog 0 -16          # Set tap to -16
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/tonylturner/dnp3go"
)

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: dnp3cmd <host:port> -a <addr> [mode] <command>

Commands:
  crob <index> <trip|close|latch-on|latch-off>   Binary output control (CROB)
  analog <index> <value>                          Analog output (float)

Options:
  -a <addr>    Outstation address (required)
  -m <addr>    Master address (default: 100)

Control mode (default: Direct Operate, FC 0x05):
  -sbo         Select-Before-Operate: Select (FC 0x03) then Operate (FC 0x04)
  -no-ack      Direct Operate, No Acknowledge (FC 0x06): fire-and-forget, no reply

Examples:
  dnp3cmd 10.40.40.20:20000 -a 1 crob 0 trip
  dnp3cmd 10.40.40.21:20000 -a 2 crob 1 latch-off
  dnp3cmd 10.40.40.20:20000 -a 1 -sbo crob 0 trip
  dnp3cmd 10.40.40.20:20000 -a 1 -no-ack crob 0 trip
  dnp3cmd 10.40.40.22:20000 -a 3 analog 0 -16
`)
	os.Exit(1)
}

// controlMode selects which DNP3 function-code sequence a control uses.
type controlMode int

const (
	modeDirectOperate controlMode = iota // FC 0x05
	modeSelectOperate                    // FC 0x03 then FC 0x04
	modeDirectNoAck                      // FC 0x06
)

func main() {
	if len(os.Args) < 6 {
		usage()
	}

	// Manual arg parsing to support positional + flag mix
	endpoint := ""
	outstationAddr := 0
	masterAddr := 100
	mode := modeDirectOperate
	var cmdArgs []string

	i := 1
	for i < len(os.Args) {
		arg := os.Args[i]
		switch arg {
		case "-a":
			i++
			if i >= len(os.Args) {
				usage()
			}
			v, err := strconv.Atoi(os.Args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Bad outstation address: %s\n", os.Args[i])
				os.Exit(1)
			}
			outstationAddr = v
		case "-m":
			i++
			if i >= len(os.Args) {
				usage()
			}
			v, err := strconv.Atoi(os.Args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Bad master address: %s\n", os.Args[i])
				os.Exit(1)
			}
			masterAddr = v
		case "-sbo":
			mode = modeSelectOperate
		case "-no-ack":
			mode = modeDirectNoAck
		default:
			if endpoint == "" {
				endpoint = arg
			} else {
				cmdArgs = append(cmdArgs, arg)
			}
		}
		i++
	}

	if endpoint == "" || outstationAddr == 0 || len(cmdArgs) < 2 {
		usage()
	}

	switch cmdArgs[0] {
	case "crob":
		doCROB(endpoint, uint16(outstationAddr), uint16(masterAddr), mode, cmdArgs[1:])
	case "analog":
		doAnalog(endpoint, uint16(outstationAddr), uint16(masterAddr), mode, cmdArgs[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s (use 'crob' or 'analog')\n", cmdArgs[0])
		os.Exit(1)
	}
}

var crobCodes = map[string]uint8{
	"trip":      dnp3go.CROBTripPulse,
	"close":     dnp3go.CROBClosePulse,
	"latch-on":  dnp3go.CROBLatchOn,
	"latch-off": dnp3go.CROBLatchOff,
	"pulse-on":  dnp3go.CROBPulseOn,
	"pulse-off": dnp3go.CROBPulseOff,
}

func modeLabel(mode controlMode) string {
	switch mode {
	case modeSelectOperate:
		return "Select-Before-Operate"
	case modeDirectNoAck:
		return "Direct Operate (No Ack)"
	default:
		return "Direct Operate"
	}
}

func doCROB(endpoint string, outAddr, masterAddr uint16, mode controlMode, args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: dnp3cmd <host:port> -a <addr> crob <index> <trip|close|latch-on|latch-off>\n")
		os.Exit(1)
	}

	index, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bad index: %s\n", args[0])
		os.Exit(1)
	}

	cc, ok := crobCodes[args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown CROB action: %s\nValid: trip, close, latch-on, latch-off, pulse-on, pulse-off\n", args[1])
		os.Exit(1)
	}

	fmt.Printf("DNP3 %s → %s (outstation %d)\n", modeLabel(mode), endpoint, outAddr)
	fmt.Printf("  CROB index %d, action: %s (0x%02X)\n", index, args[1], cc)

	build := func(funcCode, seq uint8) []byte {
		return buildCROBAPDU(funcCode, seq, uint8(index), cc)
	}
	resp := runControl(endpoint, outAddr, masterAddr, mode, build)
	fmt.Printf("  Result: %s\n", resp)
}

func doAnalog(endpoint string, outAddr, masterAddr uint16, mode controlMode, args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: dnp3cmd <host:port> -a <addr> analog <index> <value>\n")
		os.Exit(1)
	}

	index, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bad index: %s\n", args[0])
		os.Exit(1)
	}

	value, err := strconv.ParseFloat(args[1], 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Bad value: %s\n", args[1])
		os.Exit(1)
	}

	fmt.Printf("DNP3 %s → %s (outstation %d)\n", modeLabel(mode), endpoint, outAddr)
	fmt.Printf("  Analog Output index %d = %.1f\n", index, value)

	build := func(funcCode, seq uint8) []byte {
		return buildAnalogAPDU(funcCode, seq, uint8(index), float32(value))
	}
	resp := runControl(endpoint, outAddr, masterAddr, mode, build)
	fmt.Printf("  Result: %s\n", resp)
}

// appControl returns an application control byte (FIR=1, FIN=1) carrying the
// given application sequence number (0-15).
func appControl(seq uint8) byte {
	return 0xC0 | (seq & 0x0F)
}

func buildCROBAPDU(funcCode, seq, index, controlCode uint8) []byte {
	apdu := []byte{
		appControl(seq), // AC: FIR=1, FIN=1, SEQ
		funcCode,
		dnp3go.GroupCROB, // Group 12
		dnp3go.VarCROB,   // Var 1
		0x17,             // Qualifier: count8+prefix8
		1,                // Count
		index,            // Point index
	}
	// CROB: control(1) + count(1) + onTime(4) + offTime(4) + status(1)
	crob := make([]byte, 11)
	crob[0] = controlCode
	crob[1] = 1
	binary.LittleEndian.PutUint32(crob[2:6], 1000)
	binary.LittleEndian.PutUint32(crob[6:10], 1000)
	crob[10] = 0
	return append(apdu, crob...)
}

func buildAnalogAPDU(funcCode, seq, index uint8, value float32) []byte {
	apdu := []byte{
		appControl(seq),
		funcCode,
		dnp3go.GroupAnalogOutputCmd, // Group 41
		dnp3go.VarAOCmdFloat,        // Var 3
		0x17,                        // Qualifier
		1,                           // Count
		index,                       // Point index
	}
	ao := make([]byte, 5)
	ao[0] = 0 // status
	binary.LittleEndian.PutUint32(ao[1:], math.Float32bits(value))
	return append(apdu, ao...)
}

// buildAPDU produces a control APDU for the given function code and app sequence.
type buildAPDU func(funcCode, seq uint8) []byte

// runControl drives a control over a single connection according to the mode:
//   - Direct Operate (FC 0x05): one request, one response.
//   - Direct Operate No Ack (FC 0x06): one request, no response expected.
//   - Select-Before-Operate: Select (FC 0x03) then Operate (FC 0x04), each
//     with its own response; the Operate response carries the final status.
func runControl(endpoint string, outAddr, masterAddr uint16, mode controlMode, build buildAPDU) string {
	conn, err := net.DialTimeout("tcp", endpoint, 3*time.Second)
	if err != nil {
		return fmt.Sprintf("FAILED: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	switch mode {
	case modeDirectNoAck:
		if err := writeAPDU(conn, outAddr, masterAddr, 0, build(dnp3go.FCDirectOperateNoAck, 0)); err != nil {
			return fmt.Sprintf("FAILED: write: %v", err)
		}
		// No reply is expected for FC 0x06.
		return "SENT (no acknowledgement requested)"

	case modeSelectOperate:
		// Select (FC 0x03), app sequence 0.
		if err := writeAPDU(conn, outAddr, masterAddr, 0, build(dnp3go.FCSelect, 0)); err != nil {
			return fmt.Sprintf("FAILED: select write: %v", err)
		}
		selStatus, err := readControlStatus(conn)
		if err != nil {
			return fmt.Sprintf("FAILED: select: %v", err)
		}
		if selStatus != statusSuccess {
			return fmt.Sprintf("SELECT %s", statusText(selStatus))
		}
		// Operate (FC 0x04), app sequence 1.
		if err := writeAPDU(conn, outAddr, masterAddr, 1, build(dnp3go.FCOperate, 1)); err != nil {
			return fmt.Sprintf("FAILED: operate write: %v", err)
		}
		opStatus, err := readControlStatus(conn)
		if err != nil {
			return fmt.Sprintf("FAILED: operate: %v", err)
		}
		return statusText(opStatus)

	default: // modeDirectOperate
		if err := writeAPDU(conn, outAddr, masterAddr, 0, build(dnp3go.FCDirectOperate, 0)); err != nil {
			return fmt.Sprintf("FAILED: write: %v", err)
		}
		status, err := readControlStatus(conn)
		if err != nil {
			return fmt.Sprintf("FAILED: %v", err)
		}
		return statusText(status)
	}
}

// writeAPDU wraps an APDU in transport+link layers and sends it.
func writeAPDU(conn net.Conn, outAddr, masterAddr uint16, tpSeq uint8, apdu []byte) error {
	frame := &dnp3go.LinkFrame{
		Control: 0xC4,
		Dest:    outAddr,
		Source:  masterAddr,
		Payload: dnp3go.WrapTransport(apdu, tpSeq),
	}
	return dnp3go.WriteLinkFrame(conn, frame)
}

// controlStatus is the CROB/analog status code from a control response, or a
// sentinel when none was present.
type controlStatus int

const (
	statusSuccess    controlStatus = 0x00
	statusNoResponse controlStatus = -1 // response had no status object
	statusOutstaErr  controlStatus = -2 // IIN reported an error
)

var lastIIN uint16

// readControlStatus reads one response frame and extracts the control status.
func readControlStatus(conn net.Conn) (controlStatus, error) {
	respFrame, err := dnp3go.ReadLinkFrame(conn)
	if err != nil {
		return statusNoResponse, fmt.Errorf("read response: %w", err)
	}

	tr := &dnp3go.TransportReader{}
	appData := tr.ProcessSegment(respFrame.Payload)
	if appData == nil {
		return statusNoResponse, fmt.Errorf("incomplete transport")
	}

	respAPDU, err := dnp3go.ParseAPDU(appData)
	if err != nil {
		return statusNoResponse, fmt.Errorf("parse: %w", err)
	}
	lastIIN = respAPDU.IIN

	// IIN1.7 (Device Restart, 0x8000) is informational on first contact, not
	// an error — mask it before checking for real fault bits.
	if respAPDU.IIN&^(uint16(dnp3go.IIN1DeviceRestart)<<8) != 0 {
		return statusOutstaErr, nil
	}

	for _, oh := range respAPDU.Objects {
		for _, po := range oh.PrefixedData {
			if len(po.Data) > 0 {
				return controlStatus(po.Data[len(po.Data)-1]), nil
			}
		}
	}
	return statusNoResponse, nil
}

// statusText renders a control status for display.
func statusText(s controlStatus) string {
	switch s {
	case statusSuccess:
		return "SUCCESS"
	case statusNoResponse:
		return "OK (response received, no status object)"
	case statusOutstaErr:
		return fmt.Sprintf("OUTSTATION ERROR: IIN=0x%04X", lastIIN)
	case 0x07:
		return "BLOCKED (lockout or remote control disabled)"
	case 0x04:
		return "NOT SUPPORTED"
	case 0x02:
		return "NO PRIOR SELECT"
	default:
		return fmt.Sprintf("STATUS: 0x%02X", int(s))
	}
}
