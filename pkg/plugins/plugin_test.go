package plugins // import "github.com/docker/docker/pkg/plugins"

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/pkg/plugins/transport"
	"github.com/docker/go-connections/tlsconfig"
	"github.com/pkg/errors"
	"gotest.tools/v3/assert"
)

const (
	fruitPlugin     = "fruit"
	fruitImplements = "apple"
)

// regression test for deadlock in handlers
func TestPluginAddHandler(t *testing.T) {
	t.Parallel()
	// make a plugin which is pre-activated
	p := &Plugin{activateWait: sync.NewCond(&sync.Mutex{})}
	p.Manifest = &Manifest{Implements: []string{"bananas"}}
	storage.Lock()
	storage.plugins["qwerty"] = p
	storage.Unlock()

	testActive(t, p)
	Handle("bananas", func(_ string, _ *Client) {})
	testActive(t, p)
}

func TestPluginWaitBadPlugin(t *testing.T) {
	p := &Plugin{activateWait: sync.NewCond(&sync.Mutex{})}
	p.activateErr = errors.New("some junk happened")
	testActive(t, p)
}

func testActive(t *testing.T, p *Plugin) {
	done := make(chan struct{})
	go func() {
		p.waitActive()
		close(done)
	}()

	select {
	case <-time.After(100 * time.Millisecond):
		_, f, l, _ := runtime.Caller(1)
		t.Fatalf("%s:%d: deadlock in waitActive", filepath.Base(f), l)
	case <-done:
	}
}

func TestGet(t *testing.T) {
	// TODO: t.Parallel()
	// TestPluginWithNoManifest also registers fruitPlugin

	p := &Plugin{name: fruitPlugin, activateWait: sync.NewCond(&sync.Mutex{})}
	p.Manifest = &Manifest{Implements: []string{fruitImplements}}
	storage.Lock()
	storage.plugins[fruitPlugin] = p
	storage.Unlock()

	t.Run("success", func(t *testing.T) {
		plugin, err := Get(fruitPlugin, fruitImplements)
		if err != nil {
			t.Fatal(err)
		}
		if p.Name() != plugin.Name() {
			t.Errorf("no matching plugin with name %s found", plugin.Name())
		}
		if plugin.Client() != nil {
			t.Error("expected nil Client but found one")
		}
		if !plugin.IsV1() {
			t.Error("Expected true for V1 plugin")
		}
	})

	// check negative case where plugin fruit doesn't implement banana
	t.Run("not implemented", func(t *testing.T) {
		_, err := Get("fruit", "banana")
		assert.Assert(t, errors.Is(err, ErrNotImplements))
	})

	// check negative case where plugin vegetable doesn't exist
	t.Run("not exists", func(t *testing.T) {
		_, err := Get("vegetable", "potato")
		assert.Assert(t, errors.Is(err, ErrNotFound))
	})
}

func TestPluginWithNoManifest(t *testing.T) {
	// TODO: t.Parallel()
	// TestGet also registers fruitPlugin
	mux, addr := setupRemotePluginServer(t)

	m := Manifest{[]string{fruitImplements}}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(m); err != nil {
		t.Fatal(err)
	}

	mux.HandleFunc("/Plugin.Activate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("Expected POST, got %s\n", r.Method)
		}

		header := w.Header()
		header.Set("Content-Type", transport.VersionMimetype)

		io.Copy(w, &buf)
	})

	p := &Plugin{
		name:         fruitPlugin,
		activateWait: sync.NewCond(&sync.Mutex{}),
		Addr:         addr,
		TLSConfig:    &tlsconfig.Options{InsecureSkipVerify: true},
	}
	storage.Lock()
	storage.plugins[fruitPlugin] = p
	storage.Unlock()

	plugin, err := Get(fruitPlugin, fruitImplements)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != plugin.Name() {
		t.Fatalf("No matching plugin with name %s found", plugin.Name())
	}
}

func TestGetAll(t *testing.T) {
	t.Parallel()

	tmpdir := t.TempDir()
	r := LocalRegistry{
		socketsPath: tmpdir,
		specsPaths:  []string{tmpdir},
	}

	p := filepath.Join(tmpdir, "example.json")
	spec := `{
	"Name": "example",
	"Addr": "https://example.com/docker/plugin"
}`

	if err := os.WriteFile(p, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	plugin, err := r.Plugin("example")
	if err != nil {
		t.Fatal(err)
	}
	plugin.Manifest = &Manifest{Implements: []string{"apple"}}
	storage.Lock()
	storage.plugins["example"] = plugin
	storage.Unlock()

	fetchedPlugins, err := r.GetAll("apple")
	if err != nil {
		t.Fatal(err)
	}
	if fetchedPlugins[0].Name() != plugin.Name() {
		t.Fatalf("Expected to get plugin with name %s", plugin.Name())
	}
}
