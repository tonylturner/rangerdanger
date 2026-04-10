package main

// Autonomous NTP client broadcasts — the GPS-sync'd time source in a real
// substation distributes time to field devices (protection relays,
// reclosers, regulators) so sequence-of-events records are consistently
// timestamped to a common wall clock.
//
// In production this usually happens via IRIG-B hardwiring or NTP/SNTP,
// with the field device acting as NTP client and the GPS as NTP server.
// Our Alpine-based simulator field devices don't run an NTP daemon, so
// we invert the direction: the GPS sends NTP client packets AT the
// field devices, modeling a time-distribution pattern. The field
// devices reply with ICMP port unreachable (there is no NTP listener),
// which is fine — the point is to create observable cross-zone NTP
// traffic that the firewall can see and rule on.
//
// This flow is cross-zone (OT Ops 10.30.30.50 → field 10.40.40.x) so it
// traverses the firewall and is visible in the capture without any
// help from the traffic generator.

import (
	"log"
	"net"
	"time"
)

// broadcastNTP runs as a goroutine and sends NTP v3 client packets to a
// list of field device IPs at a steady interval. It respects the
// state.NTPEnabled flag so students can disable it in scenarios that
// test time-source failure.
func broadcastNTP() {
	// Give the rest of the stack a moment to come up before first send
	time.Sleep(5 * time.Second)

	targets := []string{
		"10.40.40.20:123", // relay
		"10.40.40.21:123", // recloser
		"10.40.40.22:123", // regulator
		"10.40.40.23:123", // capbank
		"10.30.30.20:123", // rtac (intra-zone, not visible at firewall)
	}

	// NTPv3 client request: Leap=0, Version=3, Mode=3 → first byte 0x1b.
	// Rest of the 48-byte packet is zero (no stratum, no root delay, etc).
	ntpPacket := make([]byte, 48)
	ntpPacket[0] = 0x1b

	interval := 12 * time.Second
	log.Printf("NTP client broadcast started (interval=%s, targets=%d)", interval, len(targets))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		state.mu.RLock()
		enabled := state.NTPEnabled
		state.mu.RUnlock()
		if !enabled {
			continue
		}

		for _, target := range targets {
			conn, err := net.DialTimeout("udp", target, 1*time.Second)
			if err != nil {
				continue
			}
			_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
			_, _ = conn.Write(ntpPacket)
			_ = conn.Close()
		}
	}
}
