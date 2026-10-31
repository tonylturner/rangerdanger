package labs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

var iecPlanLists = []string{"typeIds", "classes", "ca", "ioa", "cot", "originators", "commandStates", "commandQualifiers"}
var iecPlanScalars = []string{"negative", "test", "selectExecute"}
var iecForbiddenKeys = []string{"functionCode", "unitId", "addresses", "objectClasses", "writeOnly"}

// The raw-node check runs before decoding can erase null, false or empty
// selectors. Both input formats retain strict unknown-field rejection.
func (predicate *PlanICS) UnmarshalYAML(node *yaml.Node) error {
	if err := checkPlanICSNode(node); err != nil {
		return err
	}
	type plain PlanICS
	var decoded plain
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	if err := decodeStrict(data, &decoded); err != nil {
		return err
	}
	*predicate = PlanICS(decoded)
	return validatePlanICS(*predicate)
}

func (predicate *PlanICS) UnmarshalJSON(data []byte) error {
	type plain PlanICS
	var decoded plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if err := checkPlanICSNode(document.Content[0]); err != nil {
		return err
	}
	*predicate = PlanICS(decoded)
	return validatePlanICS(*predicate)
}

func checkPlanICSNode(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("ics must be an object")
	}
	fields := map[string]*yaml.Node{}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if !slices.Contains(iecPlanLists, key) && !slices.Contains(iecPlanScalars, key) &&
			!slices.Contains(iecForbiddenKeys, key) &&
			!slices.Contains([]string{"protocol", "readOnly", "direction", "mode"}, key) {
			return fmt.Errorf("unknown ics field %s", key)
		}
		if fields[key] != nil {
			return fmt.Errorf("duplicate ics key %s", key)
		}
		fields[key] = node.Content[i+1]
	}
	protocol := ""
	if field := fields["protocol"]; field != nil {
		protocol = field.Value
	}
	if protocol == "iec104" {
		for _, key := range iecForbiddenKeys {
			if fields[key] != nil {
				return fmt.Errorf("ics %s is forbidden on iec104 by presence", key)
			}
		}
	}
	for _, keys := range [][]string{iecPlanLists, iecPlanScalars} {
		for _, key := range keys {
			field := fields[key]
			if field == nil {
				continue
			}
			if protocol != "iec104" {
				return fmt.Errorf("ics %s is iec104-only", key)
			}
			if field.Tag == "!!null" {
				return fmt.Errorf("ics %s must not be null", key)
			}
			if (key == "negative" || key == "test") && field.Tag != "!!bool" {
				return fmt.Errorf("ics %s must be a boolean", key)
			}
			if key == "selectExecute" && field.Tag != "!!str" {
				return fmt.Errorf("ics selectExecute must be a string")
			}
			if slices.Contains(iecPlanLists, key) {
				if field.Kind != yaml.SequenceNode || len(field.Content) == 0 {
					return fmt.Errorf("ics %s must be a nonempty array", key)
				}
				for _, item := range field.Content {
					if item.Tag == "!!null" {
						return fmt.Errorf("ics %s must not contain null", key)
					}
					if key == "classes" && item.Tag != "!!str" {
						return fmt.Errorf("ics classes must contain strings")
					}
					if key != "classes" && key != "ioa" && item.Tag != "!!int" {
						return fmt.Errorf("ics %s must contain integers", key)
					}
					if key == "ioa" {
						if err := checkPlanIOANode(item); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	if field := fields["selectExecute"]; field != nil && field.Value == "" {
		return fmt.Errorf("ics selectExecute must be select or execute")
	}
	return nil
}

func checkPlanIOANode(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode || len(node.Content) != 4 {
		return fmt.Errorf("ics ioa requires exactly start and end")
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if (key != "start" && key != "end") || seen[key] || value.Tag != "!!int" {
			return fmt.Errorf("ics ioa requires integer start and end")
		}
		seen[key] = true
	}
	return nil
}

func validatePlanICS(predicate PlanICS) error {
	if err := validateGenericPlanICS(predicate); err != nil {
		return err
	}
	if predicate.Protocol != "iec104" {
		if predicate.TypeIDs != nil || predicate.Classes != nil || predicate.CA != nil ||
			predicate.IOA != nil || predicate.COT != nil || predicate.Originators != nil ||
			predicate.Negative != nil || predicate.Test != nil || predicate.SelectExecute != "" ||
			predicate.CommandStates != nil || predicate.CommandQualifiers != nil {
			return fmt.Errorf("ics IEC selectors are iec104-only")
		}
		return nil
	}
	if predicate.FunctionCode != nil || predicate.UnitID != nil || predicate.Addresses != nil ||
		predicate.ObjectClasses != nil || predicate.WriteOnly != nil {
		return fmt.Errorf("ics generic selectors are forbidden on iec104")
	}
	for _, field := range []struct {
		name     string
		values   []int
		min, max int
	}{
		{"typeIds", predicate.TypeIDs, 1, 255},
		{"ca", predicate.CA, 0, 65534},
		{"cot", predicate.COT, 1, 63},
		{"originators", predicate.Originators, 0, 255},
		{"commandQualifiers", predicate.CommandQualifiers, 0, 31},
	} {
		if field.values != nil && len(field.values) == 0 {
			return fmt.Errorf("ics %s must not be empty", field.name)
		}
		for _, value := range field.values {
			if value < field.min || value > field.max {
				return fmt.Errorf("ics %s must be in %d..%d", field.name, field.min, field.max)
			}
		}
	}
	if predicate.Classes != nil && len(predicate.Classes) == 0 {
		return fmt.Errorf("ics classes must not be empty")
	}
	for _, class := range predicate.Classes {
		if !slices.Contains([]string{"monitor", "control", "system", "unknown"}, class) {
			return fmt.Errorf("invalid ics class %q", class)
		}
	}
	if predicate.IOA != nil && len(predicate.IOA) == 0 {
		return fmt.Errorf("ics ioa must not be empty")
	}
	for i, span := range predicate.IOA {
		if span.Start < 0 || span.End > 16777215 || span.Start > span.End {
			return fmt.Errorf("ics ioa must be ordered in 0..16777215")
		}
		for _, previous := range predicate.IOA[:i] {
			if span.Start <= previous.End && previous.Start <= span.End {
				return fmt.Errorf("ics ioa ranges must not overlap")
			}
		}
	}
	subset := func(allowed []int) bool {
		for _, id := range predicate.TypeIDs {
			if !slices.Contains(allowed, id) {
				return false
			}
		}
		return true
	}
	if predicate.SelectExecute != "" {
		if !slices.Contains([]string{"select", "execute"}, predicate.SelectExecute) ||
			!subset([]int{45, 46, 47, 48, 49, 50, 58, 59, 60, 61, 62, 63}) {
			return fmt.Errorf("ics selectExecute requires select/execute and compatible typeIds")
		}
	}
	if predicate.CommandStates != nil {
		if len(predicate.CommandStates) == 0 || len(predicate.TypeIDs) == 0 {
			return fmt.Errorf("ics commandStates requires nonempty states and explicit typeIds")
		}
		min, max := 0, 1
		if !subset([]int{45, 58}) {
			min, max = 1, 2
			if !subset([]int{46, 47, 59, 60}) {
				return fmt.Errorf("ics commandStates requires typeIds in one state domain")
			}
		}
		for _, state := range predicate.CommandStates {
			if state < min || state > max {
				return fmt.Errorf("ics commandStates must be in %d..%d", min, max)
			}
		}
	}
	if predicate.CommandQualifiers != nil &&
		(len(predicate.TypeIDs) == 0 || !subset([]int{45, 46, 47, 58, 59, 60})) {
		return fmt.Errorf("ics commandQualifiers requires explicit compatible typeIds")
	}
	return nil
}
