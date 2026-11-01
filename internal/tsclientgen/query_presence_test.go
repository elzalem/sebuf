package tsclientgen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SebastienMelki/sebuf/internal/tscommon/plugintest"
	"github.com/SebastienMelki/sebuf/internal/tscommon/typecheck"
)

// TestQueryPresenceWire runs the generated client and checks which query
// parameters it puts in the URL. The rule it pins:
//
//   - a proto3 optional field has explicit presence: every set value is sent,
//     including 0, false and "" (and a 64-bit "0"), and an unset or null field
//     is not;
//   - a field without optional has implicit presence: its zero value is
//     omitted, because proto3 cannot tell it from unset.
//
// The generated client is compiled with tsc and run under node against a fake
// fetch; the test is skipped when protoc, tsc/npx or node is missing.
func TestQueryPresenceWire(t *testing.T) {
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not found, skipping query presence wire test")
	}
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found, skipping query presence wire test")
	}
	baseDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	projectRoot := filepath.Join(baseDir, "..", "..")
	protoDir := filepath.Join(baseDir, "testdata", "proto")
	pluginPath := plugintest.Build(t, projectRoot, "protoc-gen-ts-client")

	outDir := t.TempDir()
	cmd := exec.Command("protoc",
		"--plugin=protoc-gen-ts-client="+pluginPath,
		"--ts-client_out="+outDir,
		"--ts-client_opt=paths=source_relative",
		"--proto_path="+protoDir,
		"--proto_path="+filepath.Join(projectRoot, "proto"),
		"query_presence.proto",
	)
	cmd.Dir = protoDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		t.Fatalf("protoc failed: %v\nstderr: %s", runErr, stderr.String())
	}

	// node loads the compiled modules as ES modules only under this setting.
	writeTestFile(t, filepath.Join(outDir, "package.json"), `{"type":"module"}`)
	writeTestFile(t, filepath.Join(outDir, "check.mjs"), queryPresenceScript)
	typecheck.Emit(t, outDir)
	if t.Failed() {
		return
	}

	run := exec.Command(nodeBin, "check.mjs")
	run.Dir = outDir
	output, runErr := run.CombinedOutput()
	if runErr != nil {
		t.Fatalf("query presence checks failed: %v\n%s", runErr, output)
	}
	if got := strings.TrimSpace(string(output)); got != "ok" {
		t.Fatalf("query presence checks printed %q, want ok", got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("Failed to write %s: %v", path, err)
	}
}

// queryPresenceScript calls both generated methods (GET and DELETE share the
// query builder) through a fake fetch and compares the URL it receives.
const queryPresenceScript = `
import assert from "node:assert/strict";
import { QueryPresenceServiceClient } from "./query_presence_client.js";

const urls = [];
const fakeFetch = async (url) => {
  urls.push(String(url));
  return new Response('{"id":"x"}', { status: 200, headers: { "Content-Type": "application/json" } });
};
const client = new QueryPresenceServiceClient("https://example.test", { fetch: fakeFetch });

async function query(method, req) {
  urls.length = 0;
  await client[method](req);
  return urls[0].replace("https://example.test/api/v1", "");
}

// Every optional field set to its zero value is sent; the implicit-presence
// fields at their zero value are not.
const zeroes = {
  optCount: 0, optFlag: false, optText: "", optRatio: 0, optBig: "0", optKind: "KIND_UNSPECIFIED",
  count: 0, flag: false, text: "", ratio: 0, big: "0", kind: "KIND_UNSPECIFIED",
};
const sent = "opt_count=0&opt_flag=false&opt_text=&opt_ratio=0&opt_big=0&opt_kind=KIND_UNSPECIFIED";
assert.equal(await query("search", zeroes), "/search?" + sent, "optional zero values on GET");
assert.equal(await query("remove", zeroes), "/items?" + sent, "optional zero values on DELETE");

// An unset or null optional field is not sent.
assert.equal(await query("search", {}), "/search", "nothing set");
assert.equal(
  await query("search", { optCount: undefined, optFlag: null, optText: null, optRatio: undefined }),
  "/search",
  "undefined and null optionals",
);

// Non-zero values are sent for both kinds of field.
assert.equal(
  await query("search", {
    optCount: 7, optFlag: true, optText: "a b", count: 3, flag: true, text: "x", ratio: 1.5, big: "9", kind: "KIND_PHOTO",
  }),
  "/search?opt_count=7&opt_flag=true&opt_text=a+b&count=3&flag=true&text=x&ratio=1.5&big=9&kind=KIND_PHOTO",
  "non-zero values",
);

console.log("ok");
`
