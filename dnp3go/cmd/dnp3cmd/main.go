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
	fmt.Fprintf(os.Stderr, `Usage: dnp3cmd <host:port> -a <addr> <command>

Commands:
  crob <index> <trip|close|latch-on|latch-off>   Binary output control (CROB)
  analog <index> <value>                          Analog output (float)

Options:
  -a <addr>    Outstation address (required)
  -m <addr>    Master address (default: 100)

Examples:
  dnp3cmd 10.40.40.20:20000 -a 1 crob 0 trip
  dnp3cmd 10.40.40.21:20000 -a 2 crob 1 latch-off
  dnp3cmd 10.40.40.22:20000 -a 3 analog 0 -16
`)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 6 {
		usage()
	}

	// Manual arg parsing to support positional + flag mix
	endpoint := ""
	outstationAddr := 0
	masterAddr := 100
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
		doCROB(endpoint, uint16(outstationAddr), uint16(masterAddr), cmdArgs[1:])
	case "analog":
		doAnalog(endpoint, uint16(outstationAddr), uint16(masterAddr), cmdArgs[1:])
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

func doCROB(endpoint string, outAddr, masterAddr uint16, args []string) {
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

	fmt.Printf("DNP3 Direct Operate → %s (outstation %d)\n", endpoint, outAddr)
	fmt.Printf("  CROB index %d, action: %s (0x%02X)\n", index, args[1], cc)

	resp := sendDirectOperate(endpoint, outAddr, masterAddr, buildCROBAPDU(uint8(index), cc))
	fmt.Printf("  Result: %s\n", resp)
}

func doAnalog(endpoint string, outAddr, masterAddr uint16, args []string) {
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

	fmt.Printf("DNP3 Direct Operate → %s (outstation %d)\n", endpoint, outAddr)
	fmt.Printf("  Analog Output index %d = %.1f\n", index, value)

	resp := sendDirectOperate(endpoint, outAddr, masterAddr, buildAnalogAPDU(uint8(index), float32(value)))
	fmt.Printf("  Result: %s\n", resp)
}

func buildCROBAPDU(index, controlCode uint8) []byte {
	apdu := []byte{
		0xC0,             // AC: FIR=1, FIN=1
		dnp3go.FCDirectOperate,
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

func buildAnalogAPDU(index uint8, value float32) []byte {
	apdu := []byte{
		0xC0,
		dnp3go.FCDirectOperate,
		dnp3go.GroupAnalogOutputCmd, // Group 41
		dnp3go.VarAOCmdFloat,        // Var 3
		0x17,                         // Qualifier
		1,                            // Count
		index,                        // Point index
	}
	ao := make([]byte, 5)
	ao[0] = 0 // status
	binary.LittleEndian.PutUint32(ao[1:], math.Float32bits(value))
	return append(apdu, ao...)
}

func sendDirectOperate(endpoint string, outAddr, masterAddr uint16, apdu []byte) string {
	conn, err := net.DialTimeout("tcp", endpoint, 3*time.Second)
	if err != nil {
		return fmt.Sprintf("FAILED: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	tpData := dnp3go.WrapTransport(apdu, 0)

	frame := &dnp3go.LinkFrame{
		Control: 0xC4,
		Dest:    outAddr,
		Source:  masterAddr,
		Payload: tpData,
	}

	if err := dnp3go.WriteLinkFrame(conn, frame); err != nil {
		return fmt.Sprintf("FAILED: write: %v", err)
	}

	respFrame, err := dnp3go.ReadLinkFrame(conn)
	if err != nil {
		return fmt.Sprintf("FAILED: read response: %v", err)
	}

	tr := &dnp3go.TransportReader{}
	appData := tr.ProcessSegment(respFrame.Payload)
	if appData == nil {
		return "FAILED: incomplete transport"
	}

	respAPDU, err := dnp3go.ParseAPDU(appData)
	if err != nil {
		return fmt.Sprintf("FAILED: parse: %v", err)
	}

	// Check IIN for errors
	if respAPDU.IIN&0xFF00 != 0 {
		return fmt.Sprintf("OUTSTATION ERROR: IIN=0x%04X", respAPDU.IIN)
	}

	// Look for CROB/analog status in response objects
	for _, oh := range respAPDU.Objects {
		for _, po := range oh.PrefixedData {
			if len(po.Data) > 0 {
				status := po.Data[len(po.Data)-1]
				switch status {
				case 0:
					return "SUCCESS"
				case 0x07:
					return "BLOCKED (lockout or remote control disabled)"
				case 0x04:
					return "NOT SUPPORTED"
				case 0x02:
					return "NO PRIOR SELECT"
				default:
					return fmt.Sprintf("STATUS: 0x%02X", status)
				}
			}
		}
	}

	return "OK (response received)"
}
