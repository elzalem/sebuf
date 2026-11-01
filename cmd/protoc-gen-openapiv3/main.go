package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/SebastienMelki/sebuf/internal/openapiv3"
)

// bundleConfig holds origin-level metadata for the bundled OpenAPI document.
// All fields are optional; unset fields fall through to library defaults
// (e.g. the bundled doc gets Title "API" and Version "1.0.0" if unspecified).
type bundleConfig struct {
	enabled     bool
	onlyBundle  bool
	output      string
	title       string
	version     string
	description string
	servers     []string
	contactName string
	contactURL  string
	contactMail string
	licenseName string
	licenseURL  string
}

func main() {
	req := readRequest()
	params := parseParameters(req.GetParameter())
	format := parseFormat(params)
	schema, schemaErr := parseSchemaOptions(params)
	bundle := parseBundleConfig(params)
	plugin := createPlugin(req)
	if schemaErr != nil {
		// An unknown value fails the run instead of silently producing the wrong schemas.
		plugin.Error(schemaErr)
	} else {
		generateOpenAPIFiles(plugin, format, schema, bundle)
	}
	writeResponse(plugin)
}

func readRequest() *pluginpb.CodeGeneratorRequest {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}

	var req pluginpb.CodeGeneratorRequest
	if unmarshalErr := proto.Unmarshal(input, &req); unmarshalErr != nil {
		panic(unmarshalErr)
	}
	return &req
}

func parseFormat(params map[string][]string) openapiv3.OutputFormat {
	if vs, ok := params["format"]; ok && len(vs) > 0 {
		switch vs[0] {
		case "json":
			return openapiv3.FormatJSON
		case "yaml", "yml":
			return openapiv3.FormatYAML
		}
	}
	return openapiv3.FormatYAML
}

// schemaOptions are the plugin options that change how message fields are written into
// the schemas, shared by the per-service generators and the bundle generator.
type schemaOptions struct {
	fieldNames   openapiv3.FieldNames
	nullableRefs openapiv3.NullableRefs
}

// apply configures a generator before it processes any service or message.
func (o schemaOptions) apply(generator *openapiv3.Generator) {
	generator.SetFieldNames(o.fieldNames)
	generator.SetNullableRefs(o.nullableRefs)
}

// parseSchemaOptions reads the field_names and nullable_refs options and rejects an
// unknown value of either.
func parseSchemaOptions(params map[string][]string) (schemaOptions, error) {
	fieldNames, err := parseFieldNames(params)
	if err != nil {
		return schemaOptions{}, err
	}
	nullableRefs, err := parseNullableRefs(params)
	if err != nil {
		return schemaOptions{}, err
	}
	return schemaOptions{fieldNames: fieldNames, nullableRefs: nullableRefs}, nil
}

// parseFieldNames reads the field_names option: "json" (the default) names schema
// properties by the field's json_name, "proto" by the proto field name.
func parseFieldNames(params map[string][]string) (openapiv3.FieldNames, error) {
	if vs, ok := params["field_names"]; ok && len(vs) > 0 {
		return openapiv3.ParseFieldNames(vs[0])
	}
	return openapiv3.FieldNamesJSON, nil
}

// parseNullableRefs reads the nullable_refs option: "oneof" (the default) writes a
// nullable message reference as a oneOf with a null schema, "plain" as the bare $ref.
func parseNullableRefs(params map[string][]string) (openapiv3.NullableRefs, error) {
	if vs, ok := params["nullable_refs"]; ok && len(vs) > 0 {
		return openapiv3.ParseNullableRefs(vs[0])
	}
	return openapiv3.NullableRefsOneOf, nil
}

// parseBundleConfig extracts bundle_* plugin params. Repeated keys (notably
// bundle_server) are preserved in order.
func parseBundleConfig(params map[string][]string) bundleConfig {
	cfg := bundleConfig{}

	first := func(key string) string {
		if vs, ok := params[key]; ok && len(vs) > 0 {
			return vs[0]
		}
		return ""
	}

	if v := first("bundle"); v == "true" || v == "1" {
		cfg.enabled = true
	}
	if v := first("bundle_only"); v == "true" || v == "1" {
		cfg.onlyBundle = true
	}
	cfg.output = first("bundle_output")
	cfg.title = first("bundle_title")
	cfg.version = first("bundle_version")
	cfg.description = first("bundle_description")
	cfg.servers = params["bundle_server"]
	cfg.contactName = first("bundle_contact_name")
	cfg.contactURL = first("bundle_contact_url")
	cfg.contactMail = first("bundle_contact_email")
	cfg.licenseName = first("bundle_license_name")
	cfg.licenseURL = first("bundle_license_url")

	return cfg
}

func createPlugin(req *pluginpb.CodeGeneratorRequest) *protogen.Plugin {
	opts := protogen.Options{}
	plugin, err := opts.New(req)
	if err != nil {
		panic(err)
	}
	return plugin
}

func generateOpenAPIFiles(
	plugin *protogen.Plugin,
	format openapiv3.OutputFormat,
	schema schemaOptions,
	bundle bundleConfig,
) {
	// Reject path/query params whose types cannot be represented in a URL before
	// emitting anything, so an invalid proto fails cleanly instead of documenting a
	// contract no generator implements.
	if err := openapiv3.ValidateFiles(plugin); err != nil {
		plugin.Error(err)
		return
	}

	// Per-service output (default behaviour; suppressed when bundle_only=true).
	if !bundle.enabled || !bundle.onlyBundle {
		for _, file := range plugin.Files {
			if !file.Generate {
				continue
			}
			processFileServices(plugin, file, format, schema)
		}
	}

	if bundle.enabled {
		generateBundleFile(plugin, format, schema, bundle)
	}
}

func processFileServices(
	plugin *protogen.Plugin,
	file *protogen.File,
	format openapiv3.OutputFormat,
	schema schemaOptions,
) {
	for _, service := range file.Services {
		generator := createServiceGenerator(file, service, format, schema)
		output := renderService(generator)
		writeServiceFile(plugin, service, output, format)
	}
}

func createServiceGenerator(
	_ *protogen.File,
	service *protogen.Service,
	format openapiv3.OutputFormat,
	schema schemaOptions,
) *openapiv3.Generator {
	generator := openapiv3.NewGenerator(format)
	schema.apply(generator)

	// Collect all messages referenced by this service, including those from other files
	generator.CollectReferencedMessages(service)

	generator.ProcessService(service)
	return generator
}

func renderService(generator *openapiv3.Generator) []byte {
	output, renderErr := generator.Render()
	if renderErr != nil {
		panic(renderErr)
	}
	return output
}

func writeServiceFile(
	plugin *protogen.Plugin,
	service *protogen.Service,
	output []byte,
	format openapiv3.OutputFormat,
) {
	ext := "yaml"
	if format == openapiv3.FormatJSON {
		ext = "json"
	}
	filename := fmt.Sprintf("%s.openapi.%s", service.Desc.Name(), ext)

	generatedFile := plugin.NewGeneratedFile(filename, "")
	if _, writeErr := generatedFile.Write(output); writeErr != nil {
		panic(writeErr)
	}
}

// generateBundleFile collects every service across every generated proto file into a
// single OpenAPI document with proto-package-qualified schema names.
func generateBundleFile(
	plugin *protogen.Plugin,
	format openapiv3.OutputFormat,
	schema schemaOptions,
	cfg bundleConfig,
) {
	generator := openapiv3.NewBundleGenerator(format)
	schema.apply(generator)
	applyBundleMetadata(generator, cfg)

	serviceCount := 0
	for _, file := range plugin.Files {
		if !file.Generate {
			continue
		}
		for _, service := range file.Services {
			generator.CollectReferencedMessages(service)
			generator.ProcessService(service)
			serviceCount++
		}
	}

	// No services in the protoc invocation — skip writing an empty bundle.
	if serviceCount == 0 {
		return
	}

	output := renderService(generator)
	writeBundleFile(plugin, output, format, cfg)
}

func applyBundleMetadata(g *openapiv3.Generator, cfg bundleConfig) {
	var contact *base.Contact
	if cfg.contactName != "" || cfg.contactURL != "" || cfg.contactMail != "" {
		contact = &base.Contact{
			Name:  cfg.contactName,
			URL:   cfg.contactURL,
			Email: cfg.contactMail,
		}
	}
	var license *base.License
	if cfg.licenseName != "" || cfg.licenseURL != "" {
		license = &base.License{
			Name: cfg.licenseName,
			URL:  cfg.licenseURL,
		}
	}
	g.SetInfo(cfg.title, cfg.version, cfg.description, contact, license)
	g.SetServers(cfg.servers)
}

func writeBundleFile(plugin *protogen.Plugin, output []byte, format openapiv3.OutputFormat, cfg bundleConfig) {
	filename := cfg.output
	if filename == "" {
		if format == openapiv3.FormatJSON {
			filename = "openapi.json"
		} else {
			filename = "openapi.yaml"
		}
	}
	generatedFile := plugin.NewGeneratedFile(filename, "")
	if _, writeErr := generatedFile.Write(output); writeErr != nil {
		panic(writeErr)
	}
}

func writeResponse(plugin *protogen.Plugin) {
	resp := plugin.Response()
	resp.SupportedFeatures = proto.Uint64(uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL))

	respOutput, err := proto.Marshal(resp)
	if err != nil {
		panic(err)
	}

	if _, writeErr := os.Stdout.Write(respOutput); writeErr != nil {
		panic(writeErr)
	}
}

// parseParameters parses protoc plugin parameters in the format
// "key=value,key2=value2". Repeated keys (e.g. bundle_server) collect into a slice
// in insertion order; the first value is used for scalar options.
//
// Commas inside values can be escaped with a backslash ("\,") — required for
// bundle_description and similar prose fields. The escape sequence is unescaped
// after the split.
func parseParameters(parameter string) map[string][]string {
	params := make(map[string][]string)
	if parameter == "" {
		return params
	}

	for _, pair := range splitUnescapedComma(parameter) {
		const splitLimit = 2
		kv := strings.SplitN(pair, "=", splitLimit)
		if len(kv) != splitLimit {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.ReplaceAll(strings.TrimSpace(kv[1]), `\,`, ",")
		params[key] = append(params[key], value)
	}
	return params
}

// splitUnescapedComma splits on commas but treats "\," as a literal comma.
func splitUnescapedComma(s string) []string {
	var out []string
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == ',' {
			buf.WriteByte('\\')
			buf.WriteByte(',')
			i++
			continue
		}
		if s[i] == ',' {
			out = append(out, buf.String())
			buf.Reset()
			continue
		}
		buf.WriteByte(s[i])
	}
	out = append(out, buf.String())
	return out
}
