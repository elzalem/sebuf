package csharpgen

import (
	"testing"

	"github.com/SebastienMelki/sebuf/internal/contractmodel"
)

// TestCSharpQueryConditionPresence pins when the generated client sends a
// scalar query field: a proto3 optional field (explicit presence) is sent
// whenever it is not null, zero values included; a field without optional
// (implicit presence) is sent only when it differs from its zero value. The
// execution test TestGeneratedClientsExecute checks the URLs the compiled
// client builds.
func TestCSharpQueryConditionPresence(t *testing.T) {
	scalar := func(name string) *contractmodel.TypeRef {
		return &contractmodel.TypeRef{Kind: contractmodel.KindScalar, Name: name}
	}
	enum := &contractmodel.TypeRef{Kind: contractmodel.KindEnum, Name: "Kind"}
	explicit := func(typ *contractmodel.TypeRef) *contractmodel.Field {
		return &contractmodel.Field{Type: typ, Optional: true, HasPresence: true}
	}
	implicit := func(typ *contractmodel.TypeRef) *contractmodel.Field {
		return &contractmodel.Field{Type: typ}
	}

	tests := []struct {
		name  string
		field *contractmodel.Field
		want  string
	}{
		{"optional int32", explicit(scalar("int32")), "req.X != null"},
		{"optional bool", explicit(scalar("bool")), "req.X != null"},
		{"optional string", explicit(scalar(csharpStringType)), "req.X != null"},
		{"optional double", explicit(scalar("double")), "req.X != null"},
		{"optional enum", explicit(enum), "req.X != null"},
		{"int32", implicit(scalar("int32")), "req.X != 0"},
		{"int64", implicit(scalar("int64")), "req.X != 0"},
		{"double", implicit(scalar("double")), "req.X != 0"},
		{"bool", implicit(scalar("bool")), "req.X"},
		{"string", implicit(scalar(csharpStringType)), "!string.IsNullOrEmpty(req.X)"},
		{"enum", implicit(enum), "req.X != 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := csharpQueryCondition(tt.field, "req.X"); got != tt.want {
				t.Errorf("csharpQueryCondition() = %q, want %q", got, tt.want)
			}
		})
	}
}
