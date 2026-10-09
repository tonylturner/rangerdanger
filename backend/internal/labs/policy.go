package labs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ICS rule modes a package declares in package.yml `ics_rules`.
const (
	// ICSRulesAllowed lets policies carry containd `ics` blocks.
	ICSRulesAllowed = "allowed"
	// ICSRulesForbidden rejects any `ics` key in a policy, at any depth and
	// whatever its value, so a package cannot import ICS rules its range has
	// no decoder for.
	ICSRulesForbidden = "forbidden"
)

const icsKey = "ics"

// LintPolicy checks one containd policy document: it must be a single JSON
// object, and under ICSRulesForbidden no object anywhere in it may have an
// `ics` key, even `{}` or null. file names the document in errors; each
// forbidden key is reported by its JSON pointer. The catalog loader, the
// package lint and the custom-policy apply share this one check.
func LintPolicy(file string, data []byte, icsRules string) error {
	if icsRules != ICSRulesAllowed && icsRules != ICSRulesForbidden {
		return fmt.Errorf("%s: unknown ics_rules %q", file, icsRules)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", file, err)
	}
	if _, ok := document.(map[string]any); !ok {
		return fmt.Errorf("%s: policy must be a JSON object", file)
	}
	if icsRules == ICSRulesAllowed {
		return nil
	}
	// The document is valid JSON, so the token walk cannot fail on syntax;
	// it sees every key in order, duplicates included.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var found []string
	if err := walkKeys(decoder, "", &found); err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", file, err)
	}
	if len(found) == 0 {
		return nil
	}
	problems := make([]string, len(found))
	for index, pointer := range found {
		problems[index] = fmt.Sprintf("%s: key %q at %s is forbidden (ics_rules: %s)", file, icsKey, pointer, ICSRulesForbidden)
	}
	return fmt.Errorf("%s", strings.Join(problems, "; "))
}

// walkKeys reads one JSON value and appends the pointer of every `ics`
// object key inside it to found.
func walkKeys(decoder *json.Decoder, pointer string, found *[]string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, _ := keyToken.(string)
			child := pointer + "/" + escapePointer(key)
			if key == icsKey {
				*found = append(*found, child)
			}
			if err := walkKeys(decoder, child, found); err != nil {
				return err
			}
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := walkKeys(decoder, pointer+"/"+strconv.Itoa(index), found); err != nil {
				return err
			}
		}
	}
	_, err = decoder.Token() // the closing delimiter
	return err
}

// escapePointer escapes one RFC 6901 reference token.
func escapePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

// resolvePolicies resolves package.yml `policies` from the package
// directory, keeps every file inside the definitions directory, lints each
// one, and returns the name → definitions-relative path map. Two names may
// not share a file: the active policy name is found by exact path match.
func resolvePolicies(definitionsDir, packageDir string, declared map[string]string, icsRules string) (map[string]string, error) {
	if len(declared) == 0 {
		return nil, fmt.Errorf("policies must name at least one policy file")
	}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)

	resolved := make(map[string]string, len(declared))
	owners := make(map[string]string, len(declared))
	var problems []string
	for _, name := range names {
		value := declared[name]
		if !slugPattern.MatchString(name) {
			problems = append(problems, fmt.Sprintf("policy name %q must be %s", name, slugRule))
			continue
		}
		if value == "" {
			problems = append(problems, fmt.Sprintf("policy %s: path must not be empty", name))
			continue
		}
		path := filepath.Join(packageDir, value)
		relative, err := definitionsRelative(definitionsDir, path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("policy %s %q: %v", name, value, err))
			continue
		}
		if owner, taken := owners[relative]; taken {
			problems = append(problems, fmt.Sprintf("policies %s and %s name the same file %s", owner, name, relative))
			continue
		}
		owners[relative] = name
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("policy %s: %v", name, err))
			continue
		}
		if err := LintPolicy(relative, data, icsRules); err != nil {
			problems = append(problems, fmt.Sprintf("policy %s: %v", name, err))
			continue
		}
		resolved[name] = relative
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid policies:\n%s", strings.Join(problems, "\n"))
	}
	return resolved, nil
}

// definitionsRelative returns path relative to the definitions directory,
// failing when it leaves it.
func definitionsRelative(definitionsDir, path string) (string, error) {
	relative, err := filepath.Rel(definitionsDir, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must stay inside the definitions directory")
	}
	return filepath.ToSlash(relative), nil
}
