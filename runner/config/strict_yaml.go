package config

import (
	"bytes"
	"errors"
	"io"

	"gopkg.in/yaml.v3"
)

// UnmarshalStrict decodes YAML into out, rejecting keys that have no field in
// out. A silently ignored key is how `frequency:` sat in committed benchmark
// profiles giving its call zero traffic, so an unknown key is an error rather
// than a default value.
//
// An empty document decodes to the zero value: reporting "no test_name" from
// validation is more useful than reporting EOF from the parser.
func UnmarshalStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
