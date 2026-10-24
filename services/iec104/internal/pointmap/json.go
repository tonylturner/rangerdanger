// SPDX-License-Identifier: Apache-2.0

package pointmap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
)

// Retain object order and duplicate keys: unmarshalling into a map would lose
// both the strict rejection and the C loader's first-error precedence.
type field struct {
	key string
	val any
}
type object []field

func readValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := t.(json.Delim); ok {
		switch delim {
		case '{':
			o := object{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				v, err := readValue(d)
				if err != nil {
					return nil, err
				}
				o = append(o, field{key.(string), v})
			}
			_, err = d.Token()
			return o, err
		case '[':
			a := []any{}
			for d.More() {
				v, err := readValue(d)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			_, err = d.Token()
			return a, err
		}
	}
	return t, nil
}

type parser struct{ err error }

func (p *parser) fail(format string, args ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(format, args...)
	}
}
func (o object) get(key string) (any, bool) {
	for _, f := range o {
		if f.key == key {
			return f.val, true
		}
	}
	return nil, false
}
func (p *parser) required(o object, where, key string) any {
	v, ok := o.get(key)
	if !ok {
		p.fail("%s: missing required key \"%s\"", where, key)
	}
	return v
}
func (p *parser) keys(o object, where, keys string) {
	seen := map[string]bool{}
	allowed := map[string]bool{}
	for _, key := range strings.Fields(keys) {
		allowed[key] = true
	}
	for _, f := range o {
		if seen[f.key] {
			p.fail("%s: duplicate key %s", where, f.key)
		}
		seen[f.key] = true
		if !allowed[f.key] {
			p.fail("%s: unknown key \"%s\"", where, f.key)
		}
	}
}
func (p *parser) integer(o object, where, key string, low, high int) int {
	v := p.required(o, where, key)
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < float64(low) || n > float64(high) || math.Floor(n) != n {
		p.fail("%s: \"%s\" must be an integer", where, key)
		return 0
	}
	return int(n)
}
func (p *parser) number(o object, where, key string) float64 {
	v := p.required(o, where, key)
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		p.fail("%s: \"%s\" must be a number", where, key)
	}
	return n
}
func (p *parser) boolean(o object, where, key string) bool {
	v := p.required(o, where, key)
	b, ok := v.(bool)
	if !ok {
		p.fail("%s: \"%s\" must be true or false", where, key)
	}
	return b
}
func (p *parser) str(o object, where, key string, cap int) string {
	v := p.required(o, where, key)
	s, ok := v.(string)
	if !ok {
		p.fail("%s: \"%s\" must be a string", where, key)
	} else if len(s) >= cap {
		p.fail("%s: \"%s\" is too long", where, key)
	}
	return s
}
func (p *parser) table(o object, where string) Table {
	s := p.str(o, where, "table", 16)
	if s != "input" && s != "holding" {
		p.fail("%s: \"table\" must be \"input\" or \"holding\"", where)
	}
	return Table(s)
}
func (p *parser) section(o object, where, key string) object {
	v := p.required(o, where, key)
	s, ok := v.(object)
	if !ok {
		p.fail("%s: must be an object", key)
	}
	return s
}
func (p *parser) array(o object, key string, nonempty bool) []any {
	v := p.required(o, "root", key)
	a, ok := v.([]any)
	if !ok || (nonempty && len(a) == 0) {
		p.fail("\"%s\" must be a non-empty array", key)
	}
	return a
}
func parseJSON(data []byte) (any, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("NUL")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	v, err := readValue(d)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	return v, nil
}
