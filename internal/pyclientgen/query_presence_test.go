package pyclientgen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestQueryPresenceWire runs the generated client against a fake transport and
// checks which query parameters it puts in the URL. The rule it pins:
//
//   - a proto3 optional field has explicit presence (it is None when unset):
//     every set value is sent, including 0, False and "", and None is not;
//   - a field without optional has implicit presence: its zero value is
//     omitted, because proto3 cannot tell it from unset.
func TestQueryPresenceWire(t *testing.T) {
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not found")
	}
	baseDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	projectRoot := filepath.Join(baseDir, "..", "..")
	protoDir := filepath.Join(baseDir, "testdata", "proto")
	pluginPath := filepath.Join(t.TempDir(), "protoc-gen-py-client")
	build := exec.Command("go", "build", "-o", pluginPath, "./cmd/protoc-gen-py-client")
	build.Dir = projectRoot
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build plugin: %v\n%s", buildErr, out)
	}

	outDir := t.TempDir()
	cmd := exec.Command("protoc",
		"--plugin=protoc-gen-py-client="+pluginPath,
		"--py-client_out="+outDir,
		"--py-client_opt=paths=source_relative",
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

	got := pythonRoundTrip(t, filepath.Join(outDir, "query_presence_client.py"), queryPresenceSnippet)
	if got != "ok" {
		t.Fatalf("query presence checks printed %q", got)
	}
}

// queryPresenceSnippet calls both generated methods (GET and DELETE share the
// query builder) and compares the URL the transport receives.
const queryPresenceSnippet = `
class Recorder:
    def __init__(self):
        self.urls = []

    def request(self, method, url, headers, body, timeout):
        self.urls.append(url)
        return m.HttpResponse(status=200, headers={}, body=b'{"id":"x"}')


rec = Recorder()
client = m.QueryPresenceServiceClient(
    'https://example.test', m.QueryPresenceServiceClientOptions(transport=rec))


def query(method, req):
    rec.urls.clear()
    getattr(client, method)(req)
    return rec.urls[0].replace('https://example.test/api/v1', '')


# Every optional field set to its zero value is sent; the implicit-presence
# fields at their zero value are not.
zeroes = m.QueryPresenceRequest(opt_count=0, opt_flag=False, opt_text='', opt_ratio=0.0, opt_big='0',
                                count=0, flag=False, text='', ratio=0.0)
sent = 'opt_count=0&opt_flag=false&opt_text=&opt_ratio=0.0&opt_big=0'
assert query('search', zeroes) == '/search?' + sent, query('search', zeroes)
assert query('remove', zeroes) == '/items?' + sent, query('remove', zeroes)

# An unset optional field (None) is not sent.
assert query('search', m.QueryPresenceRequest()) == '/search', query('search', m.QueryPresenceRequest())
assert query('search', m.QueryPresenceRequest(opt_count=None, opt_text=None)) == '/search'

# Non-zero values are sent for both kinds of field.
full = m.QueryPresenceRequest(opt_count=7, opt_flag=True, opt_text='a b', count=3, flag=True, text='x', ratio=1.5)
assert query('search', full) == '/search?opt_count=7&opt_flag=true&opt_text=a+b&count=3&flag=true&text=x&ratio=1.5', query('search', full)
print('ok')
`
