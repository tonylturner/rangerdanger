// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strconv"
	"strings"
)

const usage = `Usage: iec104cmd HOST[:PORT] [-oa N] [-ca N] [--json] <operation>

Operations:
  gi                            station general interrogation
  stopstart                     STOPDT then STARTDT on the open link
  dc <ioa> <open|close>         double command C_DC_NA_1
  rc <ioa> <lower|raise>        regulating step C_RC_NA_1
  sc <ioa> <on|off>             single command C_SC_NA_1
  monitor --hold SECONDS [--gi-every SECONDS]
                                hold a session, stream received ASDUs

Command options:
  -sbo            select before execute (default: direct execute)
  -delay-ms N     wait N ms between select and execute
  -mismatch       execute the opposite value to the one selected
  --json          emit JSON (JSONL for monitor); logs go to stderr
`

type arguments struct {
	host                   string
	port, oa, ca           int
	json, sbo, mismatch    bool
	delayMS, hold, giEvery int
	words                  []string
}

// C's strtol accepts leading ASCII whitespace and a sign, but no suffix.
func integer(s string, min, max int) (int, bool) {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	n, err := strconv.ParseInt(s, 10, 64)
	return int(n), err == nil && n >= int64(min) && n <= int64(max)
}

func parse(args []string) (a arguments, ok bool) {
	a = arguments{port: 2404, oa: 1, ca: 1, hold: -1}
	if len(args) < 2 || strings.HasPrefix(args[0], "-") {
		return a, false
	}
	a.host = args[0]
	if i := strings.LastIndexByte(a.host, ':'); i >= 0 {
		var valid bool
		a.port, valid = integer(a.host[i+1:], 1, 65535)
		if !valid {
			return a, false
		}
		a.host = a.host[:i]
	}
	if len(a.host) == 0 || len(a.host) >= 128 {
		return a, false
	}
	for i := 1; i < len(args); i++ {
		word := args[i]
		switch word {
		case "--json":
			a.json = true
		case "-sbo":
			a.sbo = true
		case "-mismatch":
			a.sbo, a.mismatch = true, true
		case "-oa", "-ca", "--hold", "--gi-every", "-delay-ms":
			if i+1 == len(args) {
				return a, false
			}
			i++
			min, max := 0, 255
			switch word {
			case "-ca":
				max = 65535
			case "--hold", "--gi-every":
				min, max = 1, 86400
			case "-delay-ms":
				max = 600000
			}
			n, valid := integer(args[i], min, max)
			if !valid {
				return a, false
			}
			switch word {
			case "-oa":
				a.oa = n
			case "-ca":
				a.ca = n
			case "--hold":
				a.hold = n
			case "--gi-every":
				a.giEvery = n
			case "-delay-ms":
				a.delayMS, a.sbo = n, true
			}
		default:
			if strings.HasPrefix(word, "-") || len(a.words) == 8 {
				return a, false
			}
			a.words = append(a.words, word)
		}
	}
	return a, len(a.words) > 0
}

// Operation validation deliberately happens after connecting, as in the C CLI.
func (a arguments) command() (ioa, value int, ok bool) {
	if len(a.words) != 3 {
		return 0, 0, false
	}
	ioa, ok = integer(a.words[1], 0, 0xffffff)
	if !ok {
		return 0, 0, false
	}
	values := map[string]map[string]int{
		"dc": {"open": 1, "close": 2},
		"rc": {"lower": 1, "raise": 2},
		"sc": {"off": 0, "on": 1},
	}
	value, ok = values[a.words[0]][a.words[2]]
	return
}

func (a arguments) validOperation() bool {
	switch a.words[0] {
	case "gi":
		return true
	case "monitor":
		return a.hold >= 0
	case "stopstart":
		return len(a.words) == 1
	case "dc", "rc", "sc":
		_, _, ok := a.command()
		return ok
	default:
		return false
	}
}
