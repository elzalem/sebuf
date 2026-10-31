package openapiv3_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/SebastienMelki/sebuf/internal/openapiv3"
)

const (
	fieldNamesProtoFile = "testdata/proto/field_names.proto"
	fieldNamesService   = "FieldNamesService"
)

func TestParseFieldNames(t *testing.T) {
	tests := []struct {
		value   string
		want    openapiv3.FieldNames
		wantErr bool
	}{
		{value: "", want: openapiv3.FieldNamesJSON},
		{value: "json", want: openapiv3.FieldNamesJSON},
		{value: "proto", want: openapiv3.FieldNamesProto},
		{value: "JSON", wantErr: true},
		{value: "snake", wantErr: true},
	}
	for _, tt := range tests {
		got, err := openapiv3.ParseFieldNames(tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseFieldNames(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseFieldNames(%q) = %q, want %q", tt.value, got, tt.want)
		}
		if err != nil && !strings.Contains(err.Error(), "field_names must be json or proto") {
			t.Errorf("ParseFieldNames(%q) error = %v, want the allowed values named", tt.value, err)
		}
	}
}

// buildFieldNamesPlugin builds the plugin into a temporary directory and returns its path.
func buildFieldNamesPlugin(t *testing.T) string {
	t.Helper()

	pluginPath := filepath.Join(t.TempDir(), "protoc-gen-openapiv3")
	buildCmd := exec.Command("go", "build", "-o", pluginPath, "../../cmd/protoc-gen-openapiv3")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build plugin: %v\n%s", err, out)
	}
	return pluginPath
}

// writeProtovalidateDescriptorSet writes the descriptor of buf/validate/validate.proto,
// with its imports other than the well-known types, as a FileDescriptorSet that protoc
// can import through --descriptor_set_in. The test proto marks fields required through
// that extension, and the source of that file is not part of this repository.
func writeProtovalidateDescriptorSet(t *testing.T) string {
	t.Helper()

	set := &descriptorpb.FileDescriptorSet{}
	seen := map[string]bool{}
	var add func(file protoreflect.FileDescriptor)
	add = func(file protoreflect.FileDescriptor) {
		if seen[file.Path()] || strings.HasPrefix(file.Path(), "google/protobuf/") {
			return
		}
		seen[file.Path()] = true
		imports := file.Imports()
		for i := range imports.Len() {
			add(imports.Get(i).FileDescriptor)
		}
		set.File = append(set.File, protodesc.ToFileDescriptorProto(file))
	}
	add(validate.File_buf_validate_validate_proto)

	data, err := proto.Marshal(set)
	if err != nil {
		t.Fatalf("Failed to marshal the protovalidate descriptor set: %v", err)
	}
	path := filepath.Join(t.TempDir(), "protovalidate.binpb")
	if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
		t.Fatalf("Failed to write the protovalidate descriptor set: %v", writeErr)
	}
	return path
}

// runFieldNamesProtoc runs protoc over the field_names test proto with the given plugin
// options. It returns the output directory and protoc's stderr.
func runFieldNamesProtoc(t *testing.T, pluginPath, options string) (string, string, error) {
	t.Helper()

	outDir := t.TempDir()
	cmd := exec.Command("protoc",
		"--plugin=protoc-gen-openapiv3="+pluginPath,
		"--openapiv3_out="+outDir,
		"--openapiv3_opt="+options,
		"--descriptor_set_in="+writeProtovalidateDescriptorSet(t),
		"--proto_path=testdata/proto",
		"--proto_path=../../proto",
		fieldNamesProtoFile,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return outDir, stderr.String(), err
}

// TestFieldNamesGoldenFiles compares both naming modes against golden files. The default
// mode, with the option absent and with field_names=json, must produce one and the same
// document.
func TestFieldNamesGoldenFiles(t *testing.T) {
	pluginPath := buildFieldNamesPlugin(t)

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
			goldenFile: "testdata/golden/yaml/FieldNamesService.openapi.yaml",
		},
		{
			name:       "default_json",
			options:    "format=json",
			format:     "json",
			goldenFile: "testdata/golden/json/FieldNamesService.openapi.json",
		},
		{
			name:       "explicit_json_yaml",
			options:    "format=yaml,field_names=json",
			format:     "yaml",
			goldenFile: "testdata/golden/yaml/FieldNamesService.openapi.yaml",
		},
		{
			name:       "explicit_json_json",
			options:    "format=json,field_names=json",
			format:     "json",
			goldenFile: "testdata/golden/json/FieldNamesService.openapi.json",
		},
		{
			name:       "proto_yaml",
			options:    "format=yaml,field_names=proto",
			format:     "yaml",
			goldenFile: "testdata/golden/yaml/FieldNamesService.field_names_proto.openapi.yaml",
		},
		{
			name:       "proto_json",
			options:    "format=json,field_names=proto",
			format:     "json",
			goldenFile: "testdata/golden/json/FieldNamesService.field_names_proto.openapi.json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			outDir, stderr, err := runFieldNamesProtoc(t, pluginPath, tc.options)
			if err != nil {
				t.Fatalf("protoc failed for %s: %v\nStderr: %s", tc.name, err, stderr)
			}

			generatedContent, readErr := os.ReadFile(filepath.Join(outDir, fieldNamesService+".openapi."+tc.format))
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

// collectPropertyNames gathers every name the document uses for a field: the keys of every
// properties object, every entry of a required list and every discriminator propertyName,
// at any depth.
func collectPropertyNames(node any, into map[string]bool) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			switch key {
			case "properties":
				if properties, ok := child.(map[string]any); ok {
					for name := range properties {
						into[name] = true
					}
				}
			case "required":
				if names, ok := child.([]any); ok {
					for _, name := range names {
						if s, isString := name.(string); isString {
							into[s] = true
						}
					}
				}
			case "propertyName":
				if s, ok := child.(string); ok {
					into[s] = true
				}
			}
			collectPropertyNames(child, into)
		}
	case []any:
		for _, child := range value {
			collectPropertyNames(child, into)
		}
	}
}

// TestFieldNamesProtoUsesNoJSONNames guards every place that names a field, not only the
// ones the golden file happens to exercise: in proto mode no property, required entry or
// discriminator may carry a json_name (camelCase or an override), and every proto field
// name of the test messages must appear.
func TestFieldNamesProtoUsesNoJSONNames(t *testing.T) {
	pluginPath := buildFieldNamesPlugin(t)

	outDir, stderr, err := runFieldNamesProtoc(t, pluginPath, "format=json,field_names=proto")
	if err != nil {
		t.Fatalf("protoc failed: %v\nStderr: %s", err, stderr)
	}
	content, readErr := os.ReadFile(filepath.Join(outDir, fieldNamesService+".openapi.json"))
	if readErr != nil {
		t.Fatalf("Failed to read the generated file: %v", readErr)
	}
	var doc any
	if unmarshalErr := json.Unmarshal(content, &doc); unmarshalErr != nil {
		t.Fatalf("Generated document is not JSON: %v", unmarshalErr)
	}

	names := map[string]bool{}
	collectPropertyNames(doc, names)

	for name := range names {
		if strings.ContainsAny(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ-") {
			t.Errorf("proto mode wrote the name %q, which is not a proto field name", name)
		}
	}

	// Multi-word fields of each shape, a required entry, flatten (with and without a
	// prefix), both discriminated oneofs and the json_name override.
	for _, want := range []string{
		"worker_name", "protocol_version", "is_draining", "worker_kind", "nested_probe",
		"recent_probes", "extra_labels", "capability_set", "legacy_token", "next_cursor",
		"known_probes", "last_probe", "probe_detail", "retry_count",
		"display_name", "mail_street_line", "mail_postal_code", "street_line",
		"event_id", "source_name", "body_text", "image_url", "pixel_width",
		"text_content", "image_content", "plain_value", "event_type", "payload_kind",
	} {
		if !names[want] {
			t.Errorf("proto mode did not write the name %q", want)
		}
	}
}

// TestFieldNamesDefaultKeepsJSONNames pins the default: json_name, including an override.
func TestFieldNamesDefaultKeepsJSONNames(t *testing.T) {
	pluginPath := buildFieldNamesPlugin(t)

	outDir, stderr, err := runFieldNamesProtoc(t, pluginPath, "format=json")
	if err != nil {
		t.Fatalf("protoc failed: %v\nStderr: %s", err, stderr)
	}
	content, readErr := os.ReadFile(filepath.Join(outDir, fieldNamesService+".openapi.json"))
	if readErr != nil {
		t.Fatalf("Failed to read the generated file: %v", readErr)
	}
	var doc any
	if unmarshalErr := json.Unmarshal(content, &doc); unmarshalErr != nil {
		t.Fatalf("Generated document is not JSON: %v", unmarshalErr)
	}

	names := map[string]bool{}
	collectPropertyNames(doc, names)

	for _, want := range []string{
		"workerName", "protocolVersion", "isDraining", "nestedProbe", "legacy-token",
		"mail_streetLine", "textContent", "imageContent", "plainValue", "event_type", "payload_kind",
	} {
		if !names[want] {
			t.Errorf("default mode did not write the name %q", want)
		}
	}
	for _, unwanted := range []string{"worker_name", "protocol_version", "legacy_token", "text_content"} {
		if names[unwanted] {
			t.Errorf("default mode wrote the proto name %q", unwanted)
		}
	}
}

// TestFieldNamesBundleHonorsOption checks that the bundled document follows the option too.
func TestFieldNamesBundleHonorsOption(t *testing.T) {
	pluginPath := buildFieldNamesPlugin(t)

	outDir, stderr, err := runFieldNamesProtoc(t, pluginPath,
		"format=yaml,field_names=proto,bundle=true,bundle_only=true,bundle_output=bundle.openapi.yaml")
	if err != nil {
		t.Fatalf("protoc failed: %v\nStderr: %s", err, stderr)
	}
	content, readErr := os.ReadFile(filepath.Join(outDir, "bundle.openapi.yaml"))
	if readErr != nil {
		t.Fatalf("Failed to read the bundle: %v", readErr)
	}
	if !strings.Contains(string(content), "worker_name:") {
		t.Errorf("bundle in proto mode does not hold worker_name")
	}
	if strings.Contains(string(content), "workerName:") {
		t.Errorf("bundle in proto mode holds the json name workerName")
	}
}

// TestFieldNamesRejectsUnknownValue checks that a misspelt value fails the run instead of
// silently producing the wrong names.
func TestFieldNamesRejectsUnknownValue(t *testing.T) {
	pluginPath := buildFieldNamesPlugin(t)

	outDir, stderr, err := runFieldNamesProtoc(t, pluginPath, "field_names=snake")
	if err == nil {
		t.Fatalf("protoc succeeded with field_names=snake; stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "field_names must be json or proto") {
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
