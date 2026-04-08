package main

// DNP3 master polling — the RTAC periodically sends integrity polls
// to field device outstations via DNP3. This generates real DNP3
// wire traffic visible in Wireshark and containd DPI events.
//
// The HTTP polling in pollDevices() remains the primary data source.
// DNP3 polling runs in parallel for protocol visibility.

import (
	"log"
	"os"
	"time"

	"github.com/tonylturner/dnp3go"
)

type dnp3Target struct {
	name           string
	endpoint       string
	outstationAddr uint16
}

func pollDNP3Devices() {
	// Wait for services to start
	time.Sleep(5 * time.Second)

	relayHost := envOr("RELAY_DNP3_HOST", "relay-sim:20000")
	recloserHost := envOr("RECLOSER_DNP3_HOST", "recloser-sim:20000")
	regulatorHost := envOr("REGULATOR_DNP3_HOST", "regulator-sim:20000")

	targets := []dnp3Target{
		{name: "relay", endpoint: relayHost, outstationAddr: 1},
		{name: "recloser", endpoint: recloserHost, outstationAddr: 2},
		{name: "regulator", endpoint: regulatorHost, outstationAddr: 3},
	}

	// Check if DNP3 polling is disabled (e.g., for testing without field devices)
	if os.Getenv("DISABLE_DNP3_POLL") == "1" {
		log.Printf("DNP3 master polling disabled")
		return
	}

	masterAddr := uint16(10)
	interval := 5 * time.Second // Poll every 5 seconds (less aggressive than HTTP)

	log.Printf("DNP3 master polling started (interval=%s, master_addr=%d)", interval, masterAddr)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		for _, t := range targets {
			result := dnp3go.Poll(&dnp3go.MasterConfig{
				MasterAddr:     masterAddr,
				OutstationAddr: t.outstationAddr,
				Endpoint:       t.endpoint,
				Timeout:        2 * time.Second,
			})

			if result.Error != nil {
				// Silently skip — HTTP polling handles comms status
				continue
			}

			log.Printf("DNP3 poll %s (addr %d): BI=%d BO=%d AI=%d AO=%d",
				t.name, t.outstationAddr,
				len(result.BinaryInputs), len(result.BinaryOutputs),
				len(result.AnalogInputs), len(result.AnalogOutputs))
		}
	}
}
