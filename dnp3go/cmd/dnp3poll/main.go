// dnp3poll — read DNP3 outstation points via integrity poll.
//
// Usage:
//
//	dnp3poll <host:port> -a <outstation-addr>
//	dnp3poll 10.40.40.20:20000 -a 1
//	dnp3poll 10.40.40.21:20000 -a 2 -m 100
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/tonylturner/dnp3go"
)

func main() {
	addr := flag.Int("a", 1, "outstation address")
	master := flag.Int("m", 100, "master address")
	timeout := flag.Duration("t", 3*time.Second, "connection timeout")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: dnp3poll <host:port> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Read all points from a DNP3 outstation via Class 0 integrity poll.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  dnp3poll 10.40.40.20:20000 -a 1          # Poll relay\n")
		fmt.Fprintf(os.Stderr, "  dnp3poll 10.40.40.21:20000 -a 2          # Poll recloser\n")
		fmt.Fprintf(os.Stderr, "  dnp3poll 10.40.40.22:20000 -a 3          # Poll regulator\n")
		fmt.Fprintf(os.Stderr, "  dnp3poll 10.30.30.20:20000 -a 10         # Poll RTAC\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	endpoint := flag.Arg(0)

	result := dnp3go.Poll(&dnp3go.MasterConfig{
		MasterAddr:     uint16(*master),
		OutstationAddr: uint16(*addr),
		Endpoint:       endpoint,
		Timeout:        *timeout,
	})

	if result.Error != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", result.Error)
		os.Exit(1)
	}

	fmt.Printf("DNP3 Outstation %s (addr %d) — IIN: 0x%04X\n", endpoint, *addr, result.IIN)
	fmt.Println()

	if len(result.BinaryInputs) > 0 {
		fmt.Println("Binary Inputs (Group 1):")
		keys := sortedUint16Keys(result.BinaryInputs)
		for _, idx := range keys {
			val := result.BinaryInputs[idx]
			label := "OFF"
			if val {
				label = "ON"
			}
			fmt.Printf("  [%2d] = %s\n", idx, label)
		}
		fmt.Println()
	}

	if len(result.BinaryOutputs) > 0 {
		fmt.Println("Binary Outputs (Group 10):")
		keys := sortedUint16Keys(result.BinaryOutputs)
		for _, idx := range keys {
			val := result.BinaryOutputs[idx]
			label := "OFF"
			if val {
				label = "ON"
			}
			fmt.Printf("  [%2d] = %s\n", idx, label)
		}
		fmt.Println()
	}

	if len(result.AnalogInputs) > 0 {
		fmt.Println("Analog Inputs (Group 30):")
		keys := sortedFloat32Keys(result.AnalogInputs)
		for _, idx := range keys {
			fmt.Printf("  [%2d] = %.3f\n", idx, result.AnalogInputs[idx])
		}
		fmt.Println()
	}

	if len(result.AnalogOutputs) > 0 {
		fmt.Println("Analog Outputs (Group 40):")
		keys := sortedFloat32Keys(result.AnalogOutputs)
		for _, idx := range keys {
			fmt.Printf("  [%2d] = %.3f\n", idx, result.AnalogOutputs[idx])
		}
		fmt.Println()
	}
}

func sortedUint16Keys[V any](m map[uint16]V) []uint16 {
	keys := make([]uint16, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func sortedFloat32Keys(m map[uint16]float32) []uint16 {
	return sortedUint16Keys(m)
}
