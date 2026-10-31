// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

type zoneSubnet struct {
	name string
	net  *net.IPNet
	bits int
}

type ZoneMap struct {
	zones map[string]struct{}
	cidrs []zoneSubnet
}

func ParseZones(raw string) (*ZoneMap, error) {
	result := &ZoneMap{zones: make(map[string]struct{})}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("DEVICE_ZONES must not be empty")
	}
	for _, entry := range strings.Split(raw, ",") {
		name, cidr, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(cidr) == "" {
			return nil, fmt.Errorf("invalid DEVICE_ZONES entry %q", entry)
		}
		name = strings.TrimSpace(name)
		if _, exists := result.zones[name]; exists {
			return nil, fmt.Errorf("duplicate DEVICE_ZONES zone %q", name)
		}
		ip, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil || ip.To4() == nil {
			return nil, fmt.Errorf("DEVICE_ZONES entry %q must be an IPv4 CIDR", entry)
		}
		ones, _ := network.Mask.Size()
		result.zones[name] = struct{}{}
		result.cidrs = append(result.cidrs, zoneSubnet{name: name, net: network, bits: ones})
	}
	sort.Slice(result.cidrs, func(i, j int) bool {
		if result.cidrs[i].bits == result.cidrs[j].bits {
			return result.cidrs[i].name < result.cidrs[j].name
		}
		return result.cidrs[i].bits > result.cidrs[j].bits
	})
	return result, nil
}

func (z *ZoneMap) Name(name string) string {
	if z == nil {
		return "unknown"
	}
	if _, ok := z.zones[name]; !ok {
		return "unknown"
	}
	return name
}

func (z *ZoneMap) ForPeer(peer string) string {
	if z == nil {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(peer)
	if err != nil {
		host = peer
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return "unknown"
	}
	ip = ip.To4()
	if ip == nil {
		return "unknown"
	}
	for _, zone := range z.cidrs {
		if zone.net.Contains(ip) {
			return zone.name
		}
	}
	return "unknown"
}

func (z *ZoneMap) Context(peer, protocol string) AuditContext {
	return AuditContext{Peer: peer, Protocol: protocol, Zone: z.ForPeer(peer)}
}
