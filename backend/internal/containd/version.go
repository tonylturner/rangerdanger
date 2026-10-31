package containd

import (
	"fmt"
	"regexp"
	"strings"
)

// MinimumVersion is the oldest containd release supported by this backend.
const MinimumVersion = "0.1.36"

var buildVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func checkBuild(build string) error {
	parts := buildVersion.FindStringSubmatch(build)
	if parts == nil {
		return fmt.Errorf("cannot parse containd build %q; minimum required version is %s", build, MinimumVersion)
	}
	// Semver forbids leading zeroes only in numeric prerelease identifiers.
	for _, id := range strings.Split(parts[4], ".") {
		if len(id) > 1 && id[0] == '0' && strings.Trim(id, "0123456789") == "" {
			return fmt.Errorf("cannot parse containd build %q; minimum required version is %s", build, MinimumVersion)
		}
	}
	minimum := strings.Split(MinimumVersion, ".")
	for i, floor := range minimum {
		component := parts[i+1]
		// Compare unbounded semver integers without machine integer overflow.
		if len(component) > len(floor) || len(component) == len(floor) && component > floor {
			return nil
		}
		if len(component) < len(floor) || component < floor {
			return olderBuild(build)
		}
	}
	if parts[4] != "" {
		return olderBuild(build)
	}
	return nil
}

func olderBuild(build string) error {
	return fmt.Errorf("containd %s is older than %s, the minimum this release requires", build, MinimumVersion)
}
