package openapiv3_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SebastienMelki/sebuf/internal/openapiv3"
)

const (
	nullableRefsProtoFile = "testdata/proto/nullable_refs.proto"
	nullableRefsService   = "NullableRefsService"
)

func TestParseNullableRefs(t *testing.T) {
	tests := []struct {
		value   string
		want    openapiv3.NullableRefs
		wantErr bool
	}{
		{value: "", want: openapiv3.NullableRefsOneOf},
		{value: "oneof", want: openapiv3.NullableRefsOneOf},
		{value: "plain", want: openapiv3.NullableRefsPlain},
		{value: "oneOf", wantErr: true},
		{value: "ref", wantErr: true},
	}
	for _, tt := range tests {
		got, err := openapiv3.ParseNullableRefs(tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseNullableRefs(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseNullableRefs(%q) = %q, want %q", tt.value, got, tt.want)
		}
		if err != nil && !strings.Contains(err.Error(), "nullable_refs must be oneof or plain") {
			t.Errorf("ParseNullableRefs(%q) error = %v, want the allowed values named", tt.value, err)
		}
	}
}

// runNullableRefsProtoc runs protoc over the nullable_refs test proto with the given plugin
// options. It returns the output directory and protoc's stderr.
func runNullableRefsProtoc(t *testing.T, pluginPath, options string) (string, string, error) {
	t.Helper()

	outDir := t.TempDir()
	cmd := exec.Command("protoc",
		"--plugin=protoc-gen-openapiv3="+pluginPath,
		"--openapiv3_out="+outDir,
		"--openapiv3_opt="+options,
		"--proto_path=testdata/proto",
		"--proto_path=../../proto",
		nullableRefsProtoFile,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return outDir, stderr.String(), err
}

// generateNullableRefsDocument runs the plugin in JSON format and returns the generated
// document, parsed.
func generateNullableRefsDocument(t *testing.T, pluginPath, options string) map[string]any {
	t.Helper()

	outDir, stderr, err := runNullableRefsProtoc(t, pluginPath, "format=json"+options)
	if err != nil {
		t.Fatalf("protoc failed: %v\nStderr: %s", err, stderr)
	}
	content, readErr := os.ReadFile(filepath.Join(outDir, nullableRefsService+".openapi.json"))
	if readErr != nil {
		t.Fatalf("Failed to read the generated file: %v", readErr)
	}
	var doc map[string]any
	if unmarshalErr := json.Unmarshal(content, &doc); unmarshalErr != nil {
		t.Fatalf("Generated document is not JSON: %v", unmarshalErr)
	}
	return doc
}

// accountProperties returns the properties object of the Account schema.
func accountProperties(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()

	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	account, _ := schemas["Account"].(map[string]any)
	properties, ok := account["properties"].(map[string]any)
	if !ok {
		t.Fatalf("The Account schema has no properties: %v", account)
	}
	return properties
}

// TestNullableRefsGoldenFiles compares both modes against golden files. The default, with
// the option absent and with nullable_refs=oneof, must produce one and the same document.
func TestNullableRefsGoldenFiles(t *testing.T) {
	pluginPath := buildOpenAPIPlugin(t)

	testCases := []struct {
		name       string
		options    string
		format     string
		goldenFile string
	}{
		{
			name:       "default_yaml",
			options:    "format=yaml",
			format:     "yaml",
			goldenFile: "testdata/golden/yaml/NullableRefsService.openapi.yaml",
		},
		{
			name:       "default_json",
			options:    "format=json",
			format:     "json",
			goldenFile: "testdata/golden/json/NullableRefsService.openapi.json",
		},
		{
			name:       "explicit_oneof_yaml",
			options:    "format=yaml,nullable_refs=oneof",
			format:     "yaml",
			goldenFile: "testdata/golden/yaml/NullableRefsService.openapi.yaml",
		},
		{
			name:       "explicit_oneof_json",
			options:    "format=json,nullable_refs=oneof",
			format:     "json",
			goldenFile: "testdata/golden/json/NullableRefsService.openapi.json",
		},
		{
			name:       "plain_yaml",
			options:    "format=yaml,nullable_refs=plain",
			format:     "yaml",
			goldenFile: "testdata/golden/yaml/NullableRefsService.nullable_refs_plain.openapi.yaml",
		},
		{
			name:       "plain_json",
			options:    "format=json,nullable_refs=plain",
			format:     "json",
			goldenFile: "testdata/golden/json/NullableRefsService.nullable_refs_plain.openapi.json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			outDir, stderr, err := runNullableRefsProtoc(t, pluginPath, tc.options)
			if err != nil {
				t.Fatalf("protoc failed for %s: %v\nStderr: %s", tc.name, err, stderr)
			}

			generatedContent, readErr := os.ReadFile(
				filepath.Join(outDir, nullableRefsService+".openapi."+tc.format))
			if readErr != nil {
				t.Fatalf("Failed to read the generated file: %v", readErr)
			}

			goldenContent, goldenErr := os.ReadFile(tc.goldenFile)
			if goldenErr != nil {
				if created := tryCreateGoldenFile(t, tc.goldenFile, generatedContent, goldenErr); created {
					return
				}
				t.Fatalf("Failed to read golden file %s: %v", tc.goldenFile, goldenErr)
			}

			if !bytes.Equal(generatedContent, goldenContent) {
				reportGoldenFileMismatch(t, tc.name, tc.goldenFile, generatedContent, goldenContent)
			}
		})
	}
}

// TestNullableRefsDefaultKeepsOneOf pins the default: a message reference marked
// empty_behavior=NULL is a oneOf of the reference and a null schema, and the default equals
// nullable_refs=oneof.
func TestNullableRefsDefaultKeepsOneOf(t *testing.T) {
	pluginPath := buildOpenAPIPlugin(t)

	for _, options := range []string{"", ",nullable_refs=oneof"} {
		properties := accountProperties(t, generateNullableRefsDocument(t, pluginPath, options))

		billing, _ := properties["billingAddress"].(map[string]any)
		branches, _ := billing["oneOf"].([]any)
		if len(branches) != 2 {
			t.Fatalf("options %q: billingAddress is not a two-branch oneOf: %v", options, billing)
		}
		ref, _ := branches[0].(map[string]any)
		null, _ := branches[1].(map[string]any)
		if ref["$ref"] != "#/components/schemas/Address" {
			t.Errorf("options %q: first oneOf branch is %v, want the Address reference", options, ref)
		}
		if null["type"] != "null" {
			t.Errorf("options %q: second oneOf branch is %v, want type null", options, null)
		}

		closedAt, _ := properties["closedAt"].(map[string]any)
		if _, ok := closedAt["oneOf"]; !ok {
			t.Errorf("options %q: closedAt is not a oneOf: %v", options, closedAt)
		}
	}
}

// containsNullBranch reports whether any oneOf or anyOf in the node has a schema whose type
// is exactly "null", at any depth.
func containsNullBranch(node any) bool {
	switch v := node.(type) {
	case map[string]any:
		for _, key := range []string{"oneOf", "anyOf"} {
			branches, _ := v[key].([]any)
			for _, branch := range branches {
				if schema, ok := branch.(map[string]any); ok && schema["type"] == "null" {
					return true
				}
			}
		}
		for _, child := range v {
			if containsNullBranch(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsNullBranch(child) {
				return true
			}
		}
	}
	return false
}

// TestNullableRefsPlain checks the plain mode property by property: a nullable reference is
// the bare reference and stays optional, the other message fields are unchanged, nullable
// scalars keep their type arrays, and no null schema is left inside a oneOf.
func TestNullableRefsPlain(t *testing.T) {
	pluginPath := buildOpenAPIPlugin(t)

	doc := generateNullableRefsDocument(t, pluginPath, ",nullable_refs=plain")
	properties := accountProperties(t, doc)

	if containsNullBranch(doc) {
		t.Errorf("plain mode left a null schema inside a oneOf or anyOf")
	}

	for _, name := range []string{
		"billingAddress", "shippingAddress", "mailingAddress", "defaultAddress",
	} {
		field, _ := properties[name].(map[string]any)
		if field["$ref"] != "#/components/schemas/Address" || len(field) != 1 {
			t.Errorf("%s = %v, want the bare Address reference", name, field)
		}
	}

	for _, name := range []string{"closedAt", "nickname", "note"} {
		field, _ := properties[name].(map[string]any)
		types, _ := field["type"].([]any)
		if len(types) != 2 || types[0] != "string" || types[1] != "null" {
			t.Errorf("%s has type %v, want [string, null]", name, field["type"])
		}
	}

	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	account, _ := schemas["Account"].(map[string]any)
	if required, present := account["required"]; present {
		t.Errorf("plain mode made fields required: %v", required)
	}
}

// TestNullableRefsBundleHonorsOption checks that the bundled document follows the option too.
func TestNullableRefsBundleHonorsOption(t *testing.T) {
	pluginPath := buildOpenAPIPlugin(t)

	bundleOptions := "format=yaml,bundle=true,bundle_only=true,bundle_output=bundle.openapi.yaml"
	for options, wantNull := range map[string]bool{
		bundleOptions:                          true,
		bundleOptions + ",nullable_refs=oneof": true,
		bundleOptions + ",nullable_refs=plain": false,
	} {
		outDir, stderr, err := runNullableRefsProtoc(t, pluginPath, options)
		if err != nil {
			t.Fatalf("protoc failed for %q: %v\nStderr: %s", options, err, stderr)
		}
		content, readErr := os.ReadFile(filepath.Join(outDir, "bundle.openapi.yaml"))
		if readErr != nil {
			t.Fatalf("Failed to read the bundle: %v", readErr)
		}
		if hasNull := strings.Contains(string(content), "- type: \"null\""); hasNull != wantNull {
			t.Errorf("bundle for %q: has a null oneOf branch = %v, want %v", options, hasNull, wantNull)
		}
	}
}

// TestNullableRefsRejectsUnknownValue checks that a misspelt value fails the run instead of
// silently producing the wrong schemas.
func TestNullableRefsRejectsUnknownValue(t *testing.T) {
	pluginPath := buildOpenAPIPlugin(t)

	outDir, stderr, err := runNullableRefsProtoc(t, pluginPath, "nullable_refs=ref")
	if err == nil {
		t.Fatalf("protoc succeeded with nullable_refs=ref; stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "nullable_refs must be oneof or plain") {
		t.Errorf("stderr does not name the allowed values: %s", stderr)
	}
	entries, readErr := os.ReadDir(outDir)
	if readErr != nil {
		t.Fatalf("Failed to read the output directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected option still wrote %d file(s)", len(entries))
	}
}
