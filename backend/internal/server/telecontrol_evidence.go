package server

import (
	"encoding/json"
	"strings"
	"time"
)

type ccPoint struct {
	ID      string          `json:"id"`
	Value   json.RawMessage `json:"value"`
	Quality struct {
		IV bool  `json:"iv"`
		NT bool  `json:"nt"`
		SB bool  `json:"sb"`
		BL bool  `json:"bl"`
		OV *bool `json:"ov"`
	} `json:"quality"`
	StaleReason *string   `json:"stale_reason"`
	ReceivedAt  time.Time `json:"received_at"`
}

type ccJournal struct {
	euJournal[journalDetail]
	LatestSeq uint64 `json:"latest_seq"`
	OldestSeq uint64 `json:"oldest_seq"`
}

type journalEntry = euJournalEntry[journalDetail]

type deviceTruth struct {
	Schema     int    `json:"schema"`
	Generation uint32 `json:"generation"`
	Position   string `json:"position"`
}

type auditEntry = euAuditEntry[json.RawMessage, revisionSnapshot, auditContext]

type revisionSnapshot struct {
	Revision uint64 `json:"revision"`
}

func checkPass(name, detail string) ValidationCheck {
	return ValidationCheck{name, "pass", detail}
}

func checkFail(name, detail string) ValidationCheck {
	return ValidationCheck{name, "fail", detail}
}

// badQuality lists what keeps a point from being GOOD, or "".
func badQuality(point ccPoint) string {
	var flags []string
	for _, flag := range []struct {
		set  bool
		name string
	}{{point.Quality.IV, "IV"}, {point.Quality.NT, "NT"}, {point.Quality.SB, "SB"}, {point.Quality.BL, "BL"}, {point.Quality.OV != nil && *point.Quality.OV, "OV"}} {
		if flag.set {
			flags = append(flags, flag.name)
		}
	}
	if point.StaleReason != nil {
		flags = append(flags, "stale_reason "+*point.StaleReason)
	}
	return strings.Join(flags, ", ")
}
