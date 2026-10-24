// SPDX-License-Identifier: Apache-2.0

package pointmap

import (
	"fmt"
	"io"
	"os"
)

// Load reads at most 1 MiB and validates the complete point map.
func Load(path string) (*Map, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s", path)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("%s: invalid JSON", path)
	}
	return Parse(b, path)
}

// Parse validates JSON with the same diagnostics as Load; name identifies it.
func Parse(b []byte, name string) (*Map, error) {
	if len(b) == 0 || len(b) > 1<<20 {
		return nil, fmt.Errorf("%s: empty or implausibly large", name)
	}
	v, err := parseJSON(b)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid JSON", name)
	}
	root, ok := v.(object)
	if !ok {
		return nil, fmt.Errorf("root: must be an object")
	}
	p := &parser{}
	p.keys(root, "root", "schema ca timers select_timeout_ms reconnect_interval_ms gi_interval_ms clock devices points commands")
	m := &Map{}
	m.Schema = p.integer(root, "root", "schema", 2, 2)
	m.CA = p.integer(root, "root", "ca", 1, 65535)
	t := p.section(root, "root", "timers")
	p.keys(t, "timers", "t0_s t1_s t2_s t3_s k w")
	m.Timers = Timers{
		p.integer(t, "timers", "t0_s", 1, 255),
		p.integer(t, "timers", "t1_s", 1, 255),
		p.integer(t, "timers", "t2_s", 1, 255),
		p.integer(t, "timers", "t3_s", 1, 255),
		p.integer(t, "timers", "k", 1, 32767),
		p.integer(t, "timers", "w", 1, 32767),
	}
	m.SelectTimeoutMS = p.integer(root, "root", "select_timeout_ms", 1, 600000)
	m.ReconnectIntervalMS = p.integer(root, "root", "reconnect_interval_ms", 1, 600000)
	m.GIIntervalMS = p.integer(root, "root", "gi_interval_ms", 60000, 86400000)
	m.Clock = p.clock(root)
	for _, v := range p.array(root, "devices", true) {
		if p.err != nil {
			break
		}
		if len(m.Devices) == 8 {
			p.fail("too many devices (max 8)")
			break
		}
		m.Devices = append(m.Devices, p.device(v))
	}
	for _, v := range p.array(root, "points", true) {
		if p.err != nil {
			break
		}
		if len(m.Points) == 64 {
			p.fail("too many points (max 64)")
			break
		}
		pt, device := p.point(v)
		if p.err != nil {
			break
		}
		pt.DeviceIndex = m.deviceIndex(device)
		if pt.DeviceIndex < 0 {
			p.fail("point %s: source.device \"%s\" is not a defined device", pt.ID, device)
			break
		}
		d := m.Devices[pt.DeviceIndex]
		end := pt.Address + 1
		if pt.Format == F32 {
			end++
		}
		if pt.Table != d.PollTable || pt.Address < d.PollStart || end > d.PollStart+d.PollCount ||
			(pt.Format == Step && (pt.TransientAddress < d.PollStart || pt.TransientAddress >= d.PollStart+d.PollCount)) {
			p.fail("point %s: source outside poll block", pt.ID)
		}
		if m.pointIndex(pt.ID) >= 0 {
			p.fail("duplicate point id \"%s\"", pt.ID)
		}
		m.Points = append(m.Points, pt)
	}
	if v, exists := root.get("commands"); exists && p.err == nil {
		a, ok := v.([]any)
		if !ok {
			p.fail("\"commands\" must be an array")
		}
		for _, v := range a {
			if p.err != nil {
				break
			}
			if len(m.Commands) == 16 {
				p.fail("too many commands (max 16)")
				break
			}
			m.Commands = append(m.Commands, p.command(v, m))
		}
	}
	for i, d := range m.Devices {
		for _, prev := range m.Devices[:i] {
			if prev.ID == d.ID {
				p.fail("duplicate device id")
			}
		}
	}
	ids, ioas := map[string]bool{}, map[int]bool{}
	check := func(id string, ioa int) {
		if ids[id] || ioas[ioa] {
			p.fail("duplicate IOA or id: %s", id)
		}
		ids[id], ioas[ioa] = true, true
	}
	for _, pt := range m.Points {
		check(pt.ID, pt.IOA)
	}
	for _, c := range m.Commands {
		check(c.ID, c.IOA)
	}
	if p.err != nil {
		return nil, p.err
	}
	return m, nil
}

func (m *Map) deviceIndex(id string) int {
	for i, d := range m.Devices {
		if d.ID == id {
			return i
		}
	}
	return -1
}
func (m *Map) pointIndex(id string) int {
	for i, pt := range m.Points {
		if pt.ID == id {
			return i
		}
	}
	return -1
}

func (p *parser) clock(root object) Clock {
	c := p.section(root, "root", "clock")
	p.keys(c, "clock", "url poll_ms stale_after_ms synced_status")
	out := Clock{
		URL:          p.str(c, "clock", "url", 256),
		PollMS:       p.integer(c, "clock", "poll_ms", 10, 600000),
		StaleAfterMS: p.integer(c, "clock", "stale_after_ms", 10, 600000),
	}
	v := p.required(c, "clock", "synced_status")
	a, ok := v.([]any)
	if !ok || len(a) == 0 {
		p.fail("clock: \"synced_status\" must be a non-empty array")
	}
	for _, v := range a {
		if len(out.SyncedStatus) == 8 {
			p.fail("clock: too many synced_status entries (max 8)")
			break
		}
		s, ok := v.(string)
		if !ok || len(s) >= 16 {
			p.fail("clock: each \"synced_status\" entry must be a short string")
			break
		}
		out.SyncedStatus = append(out.SyncedStatus, s)
	}
	return out
}

func (p *parser) device(v any) Device {
	d, ok := v.(object)
	if !ok {
		p.fail("devices: each entry must be an object")
	}
	p.keys(d, "device", "id host port unit poll_ms timeout_ms stale_after_ms measurements_valid poll heartbeat")
	out := Device{
		ID: p.str(d, "device", "id", 64), Host: p.str(d, "device", "host", 64),
		Port: p.integer(d, "device", "port", 1, 65535), Unit: p.integer(d, "device", "unit", 0, 255),
		PollMS:       p.integer(d, "device", "poll_ms", 10, 600000),
		TimeoutMS:    p.integer(d, "device", "timeout_ms", 10, 600000),
		StaleAfterMS: p.integer(d, "device", "stale_after_ms", 10, 600000),
	}
	v = p.required(d, "device", "poll")
	poll, ok := v.(object)
	if !ok {
		p.fail("device %s: invalid poll block", out.ID)
	}
	p.keys(poll, "poll", "table start count")
	out.PollTable = p.table(poll, "poll")
	out.PollStart = p.integer(poll, "poll", "start", 0, 65535)
	out.PollCount = p.integer(poll, "poll", "count", 1, 125)
	if out.PollStart+out.PollCount > 65536 {
		p.fail("device %s: poll block overflow", out.ID)
	}
	if v, exists := d.get("heartbeat"); exists {
		h, ok := v.(object)
		if !ok {
			p.fail("device %s: \"heartbeat\" must be an object", out.ID)
		}
		p.keys(h, "heartbeat", "address stale_after_ms")
		out.HeartbeatAddress = p.integer(h, "heartbeat", "address", out.PollStart, out.PollStart+out.PollCount-1)
		out.HeartbeatStaleMS = p.integer(h, "heartbeat", "stale_after_ms", 10, 600000)
		out.HasHeartbeat = true
	}
	if v, exists := d.get("measurements_valid"); exists {
		mv, ok := v.(object)
		if !ok {
			p.fail("device %s: \"measurements_valid\" must be an object", out.ID)
		}
		p.keys(mv, "measurements_valid", "table address bit")
		out.MVTable = p.table(mv, "measurements_valid")
		out.MVAddress = p.integer(mv, "measurements_valid", "address", 0, 65535)
		out.MVBit = p.integer(mv, "measurements_valid", "bit", 0, 15)
		if out.MVTable != out.PollTable || out.MVAddress < out.PollStart || out.MVAddress >= out.PollStart+out.PollCount {
			p.fail("device %s: measurements_valid outside poll block", out.ID)
		}
		out.HasMeasurementsValid = true
	}
	return out
}
