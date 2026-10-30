//go:build !windows

package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// An app started from the desktop doesn't get a terminal's PATH, so nvm's Node and ~/.deno's Deno
// aren't on it.
func TestRuntimesOutsidePATH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NVM_DIR", "")
	t.Setenv("FNM_DIR", "")
	t.Setenv("PATH", t.TempDir())
	script := func(rel, out string) {
		t.Helper()
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\necho "+out+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"v18.20.0", "v22.11.0", "v20.5.1", "v9.11.2"} {
		script(".nvm/versions/node/"+v+"/bin/node", v)
	}
	script(".deno/bin/deno", "deno 2.0.0")
	if err := os.WriteFile(filepath.Join(home, ".bun-not-executable"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, rt := range DetectJSRuntimes(context.Background()) {
		got[rt.Name] = rt.Path
	}
	if want := filepath.Join(home, ".nvm/versions/node/v22.11.0/bin/node"); got["node"] != want {
		t.Errorf("node = %q, want the newest, %q", got["node"], want)
	}
	if want := filepath.Join(home, ".deno/bin/deno"); got["deno"] != want {
		t.Errorf("deno = %q", got["deno"])
	}
	if _, ok := got["bun"]; ok {
		t.Errorf("found a bun that isn't there")
	}

	// Only a Node that's too old: none.
	for _, v := range []string{"v22.11.0", "v20.5.1"} {
		os.RemoveAll(filepath.Join(home, ".nvm/versions/node", v))
	}
	for _, rt := range DetectJSRuntimes(context.Background()) {
		if rt.Name == "node" {
			t.Errorf("Node 18 counts: %s", rt.Path)
		}
	}
}
