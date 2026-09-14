package configbase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Setting describes an operator-configured provider value and its wire contract.
// Plural pairs a singular API key with its optional rotation pool.
type Setting struct {
	Key             string
	Plural          string
	Secret          bool
	Token           bool
	ManualEnv       bool
	GateDoctor      bool
	Always          bool
	Default         string
	FileOrder       int
	DiscoveryOrder  int
	EnvOrder        int
	ValidationOrder int
	Resolve         func(*Config) (string, string)
	Display         func(*Config) string
}

// KeyPool declares a secret key and its rotation pool. Optional positions retain
// the historical file/discovery ordering of existing providers.
func KeyPool(key, plural string, positions ...int) Setting {
	s := Setting{Key: key, Plural: plural, Secret: true, GateDoctor: true, FileOrder: 1000, DiscoveryOrder: 1000, EnvOrder: 1000, ValidationOrder: 1000}
	if len(positions) >= 2 {
		s.FileOrder = positions[0]
		s.DiscoveryOrder = positions[1]
	}
	if len(positions) >= 3 {
		s.EnvOrder = positions[2]
	}
	if len(positions) >= 4 {
		s.ValidationOrder = positions[3]
	}
	return s
}

// Scalar declares a single non-secret operator value such as an instance URL.
// New settings append after the legacy ordered ones; providers that need doctor
// gating or a default set those fields on the returned value.
func Scalar(key string) Setting {
	return Setting{Key: key, FileOrder: 1000, DiscoveryOrder: 1000, EnvOrder: 1000, ValidationOrder: 1000}
}

// Keys returns the effective immutable, de-duplicated credential pool.
func (s Setting) Keys(c *Config) []string { return MergeKeys(c.String(s.Key), c.Strings(s.Plural)) }

// Configured reports explicit credentials that make a health check required.
func (s Setting) Configured(c *Config) bool {
	if !s.GateDoctor {
		return false
	}
	if s.Plural != "" {
		return len(s.Keys(c)) > 0
	}
	return c.String(s.Key) != ""
}

// Set validates one config-set value, returning whether this setting owns key.
func (s Setting) Set(c *Config, key, value string) (bool, error) {
	if key == s.Key {
		c.SetProvider(key, value)
		return true, nil
	}
	if s.Plural == "" || key != s.Plural {
		return false, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(value), &values); err != nil {
		return true, fmt.Errorf("%s must be a JSON array of strings: %w", key, err)
	}
	if values == nil {
		return true, fmt.Errorf("%s must be a JSON array of strings, not null", key)
	}
	c.SetProvider(key, values)
	return true, nil
}

// ApplyEnv preserves singular-key comma-list rotation and scalar overrides.
func (s Setting) ApplyEnv(c *Config, value string) error {
	if s.Plural == "" {
		c.SetProvider(s.Key, value)
		return nil
	}
	keys := MergeKeys("", strings.Split(value, ","))
	if len(keys) == 0 {
		return fmt.Errorf("must contain at least one non-blank key")
	}
	c.SetProvider(s.Key, keys[0])
	c.SetProvider(s.Plural, keys[1:])
	return nil
}

// Field is an ordered JSON field. Order is presentation metadata, not lookup
// policy. A setting's companion fields (key/plural, _set/_count) share one
// order and rely on the stable sort to stay adjacent, so two providers added
// with the same default order group per provider instead of interleaving.
type Field struct {
	Name  string
	Value any
	Order int
}

// Discovery returns public provider settings while never exposing credentials.
func (s Setting) Discovery(c *Config) []Field {
	if s.Token {
		_, source := s.Resolve(c)
		return []Field{{s.Key + "_source", source, s.DiscoveryOrder}, {s.Key + "_set", source != "none", s.DiscoveryOrder}}
	}
	if s.Plural != "" {
		n := len(s.Keys(c))
		return []Field{{s.Key + "_set", n > 0, s.DiscoveryOrder}, {s.Plural + "_count", n, s.DiscoveryOrder}}
	}
	if s.Secret {
		return []Field{{s.Key + "_set", c.String(s.Key) != "", s.DiscoveryOrder}}
	}
	value := c.String(s.Key)
	if s.Display != nil {
		value = s.Display(c)
	}
	return []Field{{s.Key, value, s.DiscoveryOrder}}
}

// WithSettings attaches a registry snapshot and fills only missing defaults.
func (c Config) WithSettings(settings []Setting) Config {
	c.providerSchema = slices.Clone(settings)
	for _, s := range settings {
		if _, ok := c.ProviderSettings[s.Key]; !ok && s.Default != "" {
			c.SetProvider(s.Key, s.Default)
		}
	}
	return c
}

// String reads a scalar provider setting without mutating shared configuration.
func (c Config) String(key string) string {
	value, _ := c.ProviderSettings[key].(string)
	return value
}

// Strings returns an immutable copy of a provider's configured key list.
func (c Config) Strings(key string) []string {
	value, _ := c.ProviderSettings[key].([]string)
	return slices.Clone(value)
}

// SetProvider copies the settings map before mutation, preserving value-copy
// semantics for per-request overrides and the shared MCP configuration.
func (c *Config) SetProvider(key string, value any) {
	values := make(map[string]any, len(c.ProviderSettings)+1)
	for k, v := range c.ProviderSettings {
		values[k] = v
	}
	if list, ok := value.([]string); ok {
		value = slices.Clone(list)
	}
	values[key] = value
	c.ProviderSettings = values
}

// MarshalFields encodes fields in stable order rather than map-key order.
func MarshalFields(fields []Field) ([]byte, error) {
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].Order < fields[j].Order })
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(f.Name)
		b.Write(key)
		b.WriteByte(':')
		value, err := json.Marshal(f.Value)
		if err != nil {
			return nil, err
		}
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// StructFields returns tagged non-provider fields with their legacy positions.
func StructFields(value any) []Field {
	v := reflect.ValueOf(value)
	typ := v.Type()
	var fields []Field
	for i := 0; i < v.NumField(); i++ {
		sf := typ.Field(i)
		tag := sf.Tag.Get("json")
		name, options, _ := strings.Cut(tag, ",")
		if sf.PkgPath != "" || name == "-" || name == "" {
			continue
		}
		field := v.Field(i)
		if options == "omitempty" && emptyField(field) {
			continue
		}
		order, _ := strconv.Atoi(sf.Tag.Get("order"))
		fields = append(fields, Field{name, field.Interface(), order})
	}
	return fields
}

func emptyField(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	default:
		return v.IsZero()
	}
}

// MarshalJSON retains the flat config-file shape with registry-owned provider fields.
func (c Config) MarshalJSON() ([]byte, error) {
	fields := StructFields(c)
	for _, s := range c.providerSchema {
		value := c.String(s.Key)
		if s.Always || value != "" {
			fields = append(fields, Field{s.Key, value, s.FileOrder})
		}
		if list := c.Strings(s.Plural); s.Plural != "" && len(list) > 0 {
			fields = append(fields, Field{s.Plural, list, s.FileOrder})
		}
	}
	if len(c.providerSchema) == 0 {
		fields = c.rawProviderFields(fields)
	}
	return MarshalFields(fields)
}

func (c Config) rawProviderFields(fields []Field) []Field {
	names := make([]string, 0, len(c.ProviderSettings))
	for name := range c.ProviderSettings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fields = append(fields, Field{name, c.ProviderSettings[name], 1000})
	}
	for i := range fields {
		if n, ok := c.providerOrder[fields[i].Name]; ok {
			fields[i].Order = n
		}
	}
	return fields
}

// UnmarshalJSON validates known provider values over existing defaults. The
// public config loader attaches the complete schema before decoding.
func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(data, (*plain)(c)); err != nil {
		return err
	}
	if len(c.providerSchema) == 0 {
		return c.decodeWithoutSchema(data, raw)
	}
	for _, s := range c.providerSchema {
		if err := c.decodeSetting(raw, s.Key, false); err != nil {
			return err
		}
		if s.Plural != "" {
			if err := c.decodeSetting(raw, s.Plural, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// lookupRaw finds key in raw the way encoding/json matches struct tags: an
// exact match wins, otherwise a case-insensitive one. Hand-edited config
// files with "BRAVE_API_KEY" or "Searxng_URL" loaded before the registry and
// must keep loading. Ties between case variants resolve in sorted key order
// so the outcome is deterministic.
func lookupRaw(raw map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	if v, ok := raw[key]; ok {
		return v, true
	}
	var match string
	for k := range raw {
		if strings.EqualFold(k, key) && (match == "" || k < match) {
			match = k
		}
	}
	if match == "" {
		return nil, false
	}
	return raw[match], true
}

func (c *Config) decodeSetting(raw map[string]json.RawMessage, key string, list bool) error {
	value, ok := lookupRaw(raw, key)
	if !ok {
		return nil
	}
	if list {
		var values []string
		if err := json.Unmarshal(value, &values); err != nil {
			return err
		}
		c.SetProvider(key, values)
		return nil
	}
	scalar := c.String(key)
	if err := json.Unmarshal(value, &scalar); err != nil {
		return err
	}
	c.SetProvider(key, scalar)
	return nil
}

func (c *Config) decodeWithoutSchema(data []byte, raw map[string]json.RawMessage) error {
	typ := reflect.TypeOf(*c)
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		delete(raw, name)
	}
	for key, value := range raw {
		if bytes.HasPrefix(bytes.TrimSpace(value), []byte("[")) {
			if err := c.decodeSetting(raw, key, true); err != nil {
				return err
			}
		} else {
			if err := c.decodeSetting(raw, key, false); err != nil {
				return err
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return err
	}
	c.providerOrder = make(map[string]int)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key := token.(string)
		c.providerOrder[key] = len(c.providerOrder)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	return nil
}

// JoinNames preserves the established human-readable provider list format.
func JoinNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}
