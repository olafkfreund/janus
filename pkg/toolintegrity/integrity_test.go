package toolintegrity

import "testing"

func baseToolDef() ToolDef {
	return ToolDef{
		Name:             "get_widget",
		Description:      "Fetches a widget by ID",
		Method:           "GET",
		Path:             "/widgets/{id}",
		ParametersSchema: `{"type":"object","properties":{"id":{"type":"string"},"verbose":{"type":"boolean"}},"required":["id"]}`,
	}
}

func TestHash_SameDefProducesSameHash(t *testing.T) {
	d := baseToolDef()

	h1 := Hash(d)
	h2 := Hash(d)

	if h1 != h2 {
		t.Fatalf("Hash is not deterministic: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Fatal("Hash returned empty string")
	}
}

func TestHash_ReorderedSchemaKeysProduceSameHash(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{
			name:   "top level keys reordered",
			schema: `{"type":"object","properties":{"id":{"type":"string"},"verbose":{"type":"boolean"}},"required":["id"]}`,
		},
		{
			name:   "nested and top level keys reordered",
			schema: `{"required":["id"],"properties":{"verbose":{"type":"boolean"},"id":{"type":"string"}},"type":"object"}`,
		},
		{
			name: "whitespace and reordering",
			schema: `
			{
				"required": ["id"],
				"type": "object",
				"properties": {
					"verbose": { "type": "boolean" },
					"id": { "type": "string" }
				}
			}`,
		},
	}

	d := baseToolDef()
	want := Hash(d)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d2 := d
			d2.ParametersSchema = tt.schema

			got := Hash(d2)
			if got != want {
				t.Errorf("Hash with reordered schema = %q, want %q (schema: %s)", got, want, tt.schema)
			}
		})
	}
}

func TestHash_FieldChangesProduceDifferentHash(t *testing.T) {
	base := baseToolDef()
	baseHash := Hash(base)

	tests := []struct {
		name   string
		mutate func(ToolDef) ToolDef
	}{
		{
			name: "description changed",
			mutate: func(d ToolDef) ToolDef {
				d.Description = "Fetches a widget by its unique ID"
				return d
			},
		},
		{
			name: "path changed",
			mutate: func(d ToolDef) ToolDef {
				d.Path = "/widgets/{widget_id}"
				return d
			},
		},
		{
			name: "method changed",
			mutate: func(d ToolDef) ToolDef {
				d.Method = "POST"
				return d
			},
		},
		{
			name: "name changed",
			mutate: func(d ToolDef) ToolDef {
				d.Name = "get_widget_v2"
				return d
			},
		},
		{
			name: "schema value changed",
			mutate: func(d ToolDef) ToolDef {
				d.ParametersSchema = `{"type":"object","properties":{"id":{"type":"integer"},"verbose":{"type":"boolean"}},"required":["id"]}`
				return d
			},
		},
		{
			name: "schema required list changed",
			mutate: func(d ToolDef) ToolDef {
				d.ParametersSchema = `{"type":"object","properties":{"id":{"type":"string"},"verbose":{"type":"boolean"}},"required":["id","verbose"]}`
				return d
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutated := tt.mutate(base)
			got := Hash(mutated)
			if got == baseHash {
				t.Errorf("Hash after mutation equals base hash %q; expected a different hash", baseHash)
			}
		})
	}
}

func TestHash_InvalidJSONSchemaIsDeterministic(t *testing.T) {
	d1 := baseToolDef()
	d1.ParametersSchema = `{not valid json at all`

	d2 := baseToolDef()
	d2.ParametersSchema = `{not valid json at all`

	h1 := Hash(d1)
	h2 := Hash(d2)
	if h1 != h2 {
		t.Fatalf("Hash of identical invalid-JSON schema not deterministic: %q != %q", h1, h2)
	}

	d3 := baseToolDef()
	d3.ParametersSchema = `{also not valid json}}}`

	h3 := Hash(d3)
	if h3 == h1 {
		t.Fatalf("Hash of different invalid-JSON schemas collided: %q", h1)
	}

	// An invalid schema must also produce a hash distinct from the valid
	// baseline schema.
	validHash := Hash(baseToolDef())
	if h1 == validHash {
		t.Fatalf("Hash of invalid schema unexpectedly matched valid schema hash: %q", h1)
	}
}

func TestChanged(t *testing.T) {
	d := baseToolDef()
	currentHash := Hash(d)

	mutated := d
	mutated.Description = "A different description entirely"

	tests := []struct {
		name      string
		priorHash string
		def       ToolDef
		want      bool
	}{
		{
			name:      "empty prior hash means no baseline, never changed",
			priorHash: "",
			def:       d,
			want:      false,
		},
		{
			name:      "empty prior hash with mutated def is still not changed",
			priorHash: "",
			def:       mutated,
			want:      false,
		},
		{
			name:      "matching prior hash means unchanged",
			priorHash: currentHash,
			def:       d,
			want:      false,
		},
		{
			name:      "mismatched prior hash means changed",
			priorHash: currentHash,
			def:       mutated,
			want:      true,
		},
		{
			name:      "arbitrary unrelated prior hash means changed",
			priorHash: "deadbeef",
			def:       d,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Changed(tt.priorHash, tt.def)
			if got != tt.want {
				t.Errorf("Changed(%q, def) = %v, want %v", tt.priorHash, got, tt.want)
			}
		})
	}
}
