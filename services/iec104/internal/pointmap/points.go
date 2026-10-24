// SPDX-License-Identifier: Apache-2.0

package pointmap

func (p *parser) kind(o object, where string, command bool) Kind {
	s := p.str(o, where, "kind", 32)
	k := Kind(s)
	valid := k == DoublePoint || k == SinglePoint || k == Float || k == StepPosition
	if command {
		valid = k == DoubleCommand || k == RegulatingStep || k == SingleCommand
	}
	if !valid {
		p.fail("%s: unsupported \"kind\" \"%s\"", where, s)
	}
	return k
}
func (p *parser) asdu(where string, kind Kind, asdu string) {
	want := map[Kind]string{
		DoublePoint: "M_DP_TB_1", SinglePoint: "M_SP_TB_1", Float: "M_ME_NC_1",
		StepPosition: "M_ST_TB_1", DoubleCommand: "C_DC_NA_1",
		RegulatingStep: "C_RC_NA_1", SingleCommand: "C_SC_NA_1",
	}[kind]
	if asdu != want {
		p.fail("%s: \"asdu\" must be \"%s\" for this kind, not \"%s\"", where, want, asdu)
	}
}

func (p *parser) point(v any) (Point, string) {
	o, ok := v.(object)
	if !ok {
		p.fail("points: each entry must be an object")
	}
	p.keys(o, "point", "id ioa kind asdu unit deadband cyclic_ms measurement source limits")
	pt := Point{
		ID:   p.str(o, "point", "id", 64),
		IOA:  p.integer(o, "point", "ioa", 1, 0xffffff),
		Kind: p.kind(o, "point", false), ASDU: p.str(o, "point", "asdu", 16),
		Scale: 1,
	}
	p.asdu(pt.ID, pt.Kind, pt.ASDU)
	if _, ok := o.get("unit"); ok {
		pt.Unit = p.str(o, "point", "unit", 16)
	}
	if _, ok := o.get("measurement"); ok {
		pt.Measurement = p.boolean(o, "point", "measurement")
	}
	if _, ok := o.get("deadband"); ok {
		pt.Deadband = p.number(o, "point", "deadband")
		pt.HasDeadband = true
		if pt.Deadband < 0 {
			p.fail("point %s: \"deadband\" must be >= 0", pt.ID)
		}
	}
	if pt.Kind != Float && (pt.HasDeadband || pt.Measurement) {
		p.fail("point %s: \"deadband\"/\"measurement\" apply only to float points", pt.ID)
	}
	if pt.Kind == Float {
		pt.CyclicMS = p.integer(o, "point", "cyclic_ms", 1000, 3600000)
	} else if _, exists := o.get("cyclic_ms"); exists {
		p.fail("point %s: \"cyclic_ms\" applies only to float points", pt.ID)
	}
	if v, exists := o.get("limits"); exists {
		l, ok := v.(object)
		if pt.Kind != Float || pt.Unit != "kV" || !ok {
			p.fail("point %s: invalid voltage limits", pt.ID)
		}
		p.keys(l, "limits", "low high")
		pt.LimitLow, pt.LimitHigh = p.number(l, "limits", "low"), p.number(l, "limits", "high")
		if pt.LimitLow >= pt.LimitHigh {
			p.fail("point %s: invalid voltage limits", pt.ID)
		}
		pt.HasLimits = true
	}
	v = p.required(o, "point", "source")
	s, ok := v.(object)
	if !ok {
		p.fail("point %s: \"source\" must be an object", pt.ID)
	}
	p.keys(s, "source", "device table address format bit scale transient")
	v = p.required(s, "source", "device")
	device, ok := v.(string)
	if !ok {
		p.fail("point %s: source.device must be a string", pt.ID)
	}
	pt.Table = p.table(s, "source")
	pt.Address = p.integer(s, "source", "address", 0, 65535)
	pt.Format = Format(p.str(s, "source", "format", 8))
	switch pt.Format {
	case DP, Bit, F32, U16, I16, Step:
	default:
		p.fail("point %s: unknown source.format", pt.ID)
	}
	if _, ok := s.get("scale"); ok {
		if pt.Format != U16 && pt.Format != I16 {
			p.fail("point %s: scale requires u16/i16", pt.ID)
		}
		pt.Scale = p.number(s, "source", "scale")
	}
	v, exists := s.get("transient")
	if pt.Format == Step {
		t, ok := v.(object)
		if !exists || !ok {
			p.fail("point %s: step requires transient address/bit", pt.ID)
		}
		p.keys(t, "transient", "address bit")
		pt.TransientAddress = p.integer(t, "transient", "address", 0, 65535)
		pt.TransientBit = p.integer(t, "transient", "bit", 0, 15)
	} else if exists {
		p.fail("point %s: transient requires step", pt.ID)
	}
	if pt.Format == Bit {
		pt.Bit = p.integer(s, "source", "bit", 0, 15)
	} else if _, ok := s.get("bit"); ok {
		p.fail("point %s: \"bit\" is only valid with format \"bit\"", pt.ID)
	}
	if (pt.Kind == DoublePoint && pt.Format != DP) ||
		(pt.Kind == SinglePoint && pt.Format != Bit) ||
		(pt.Kind == StepPosition && pt.Format != Step) ||
		(pt.Kind == Float && pt.Format != F32 && pt.Format != U16 && pt.Format != I16) {
		p.fail("point %s: kind and source.format disagree", pt.ID)
	}
	return pt, device
}

func (p *parser) command(v any, m *Map) Command {
	o, ok := v.(object)
	if !ok {
		p.fail("commands: each entry must be an object")
	}
	p.keys(o, "command", "id ioa kind asdu sbo target feedback")
	c := Command{
		ID: p.str(o, "command", "id", 64), IOA: p.integer(o, "command", "ioa", 1, 0xffffff),
		Kind: p.kind(o, "command", true), ASDU: p.str(o, "command", "asdu", 16),
	}
	p.asdu(c.ID, c.Kind, c.ASDU)
	c.SBO = p.boolean(o, "command", "sbo")
	c.FeedbackID = p.str(o, "command", "feedback", 64)
	v = p.required(o, "command", "target")
	t, ok := v.(object)
	if !ok {
		p.fail("command %s: \"target\" must be an object", c.ID)
	}
	p.keys(t, "target", "device table address")
	c.Table = p.table(t, "target")
	c.Address = p.integer(t, "target", "address", 0, 65535)
	v = p.required(t, "target", "device")
	device, ok := v.(string)
	if !ok {
		p.fail("command %s: target.device must be a string", c.ID)
	}
	c.DeviceIndex = m.deviceIndex(device)
	if c.DeviceIndex < 0 {
		p.fail("command %s: target.device \"%s\" is not a defined device", c.ID, device)
	}
	c.FeedbackIndex = m.pointIndex(c.FeedbackID)
	if c.FeedbackIndex < 0 {
		p.fail("command %s: feedback \"%s\" is not a defined point", c.ID, c.FeedbackID)
	}
	if p.err != nil {
		return c
	}
	want := map[Kind]Kind{DoubleCommand: DoublePoint, RegulatingStep: StepPosition, SingleCommand: SinglePoint}[c.Kind]
	pt := m.Points[c.FeedbackIndex]
	if c.Table != Holding || !c.SBO || pt.Kind != want || pt.DeviceIndex != c.DeviceIndex {
		p.fail("command %s: requires holding/SBO and same-device typed feedback", c.ID)
	}
	return c
}
