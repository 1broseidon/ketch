package mcp

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// inputSchema reflects In exactly as the SDK would and then overrides the
// listed property descriptions. jsonschema struct tags are compile-time
// constants, so any wording derived from the provider registries — backend
// IDs, which backends accept regex, which docs providers own library
// operations — has to be applied here, at tool registration time. Every key
// must name a real property; a typo panics at startup rather than silently
// publishing a schema with a stale description.
func inputSchema[In any](descriptions map[string]string) *jsonschema.Schema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("mcp: input schema for %T: %v", *new(In), err))
	}
	for name, d := range descriptions {
		p, ok := s.Properties[name]
		if !ok {
			panic(fmt.Sprintf("mcp: input schema for %T has no property %q", *new(In), name))
		}
		p.Description = d
	}
	return s
}
