package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// rejectDuplicateJSONKeys runs before struct/map decoding, which otherwise
// silently overwrites duplicate names. In particular, two sim entries on one
// service must fail even when authored in two separate endpoints objects.
func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return readUniqueJSONValue(decoder, "$")
}

func readUniqueJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := token.(string)
			if !ok {
				return fmt.Errorf("expected object key at %s", path)
			}
			if seen[name] {
				return fmt.Errorf("duplicate JSON key %q at %s", name, path)
			}
			seen[name] = true
			if err := readUniqueJSONValue(decoder, path+"."+name); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for index := 0; decoder.More(); index++ {
			if err := readUniqueJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = decoder.Token()
	return err
}
