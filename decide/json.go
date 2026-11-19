package decide

import (
	"bytes"
	"encoding/json"
)

// field is one member of a JSON object built in order.
type field struct {
	name  string
	value []byte // its encoded value
}

// object encodes fields as a JSON object in their order: a request's
// question order and an item's place before its question are kept.
func object(fields ...field) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		name, _ := marshal(f.name)
		b.Write(name)
		b.WriteByte(':')
		b.Write(f.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// array encodes encoded values as a JSON array.
func array(values [][]byte) []byte {
	return append(append([]byte{'['}, bytes.Join(values, []byte{','})...), ']')
}

// str is s encoded as a JSON string.
func str(s string) []byte {
	b, _ := marshal(s)
	return b
}

// value is v encoded, an error kept for the caller.
func value(v any) ([]byte, error) {
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	return marshal(v)
}
