package server

import "strings"

const euCCLinkCheckLabel = "CC link, GI and station delivery"

// evidenceDetail preserves the observation when there is no transport error.
func evidenceDetail(detail string, errs ...error) string {
	for _, err := range errs {
		if err != nil {
			detail += "; " + err.Error()
		}
	}
	return detail
}

func missingEvidenceDetail(err error) string {
	detail := err.Error()
	const prefix = "missing evidence:"
	for strings.HasPrefix(strings.ToLower(detail), prefix) {
		detail = strings.TrimSpace(detail[len(prefix):])
	}
	return "Missing evidence: " + detail
}
