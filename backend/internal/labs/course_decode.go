package labs

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML keeps omission distinct from false without accepting null as a
// fourth flag state. Decode through a plain type to retain strict field checks.
func (a *CurriculumActivity) UnmarshalYAML(node *yaml.Node) error {
	type activity CurriculumActivity
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	var value activity
	if err := decodeStrict(data, &value); err != nil {
		return err
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		flag := node.Content[i+1]
		for flag.Kind == yaml.AliasNode {
			flag = flag.Alias
		}
		if (key == "validate" || key == "policyActions" || key == "hideBanner") && flag.Tag != "!!bool" {
			return fmt.Errorf("activity %s must be a boolean or omitted", key)
		}
	}
	*a = CurriculumActivity(value)
	return nil
}
