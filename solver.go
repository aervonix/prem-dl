package main

import (
	"path/filepath"

	"yt-dlp-gui/internal/jsc"
	"yt-dlp-gui/internal/ytdlp"
)

// challengeSolver is the plugin folder and environment that let a yt-dlp run solve YouTube's
// challenges with the solver Prem-dl keeps running (internal/jsc), which saves ~0.9 s on each
// analysis and on each download that reads the page. It starts on first use. "" and nil when
// it can't be used: yt-dlp isn't an unpacked build (so it has no solver scripts to share), or
// node isn't installed; yt-dlp then solves them itself, as it does when anything here fails.
func (a *App) challengeSolver(bin string, runtimes []ytdlp.JSRuntime) (pluginDir string, vars []string) {
	scripts := jsc.ScriptsDir(bin)
	node := ""
	for _, rt := range runtimes {
		if rt.Name == "node" {
			node = rt.Path
		}
	}
	if scripts == "" || node == "" {
		return "", nil
	}
	key := node + "\x00" + scripts
	a.jscMu.Lock()
	defer a.jscMu.Unlock()
	if a.jscSrv != nil && a.jscKey == key {
		return a.jscDir, a.jscSrv.Env()
	}
	if a.jscSrv != nil {
		a.jscSrv.Close() // another node or yt-dlp; runs still using it fall back to yt-dlp's own
		a.jscSrv = nil
	}
	dir := filepath.Join(a.dataDir, "jsc-plugin")
	if err := jsc.InstallPlugin(dir); err != nil {
		a.warn("couldn't write the challenge solver's yt-dlp plugin: " + err.Error())
		return "", nil
	}
	srv, err := jsc.Serve(jsc.NewSolver(node, scripts))
	if err != nil {
		a.warn("couldn't start the challenge solver: " + err.Error())
		return "", nil
	}
	a.jscSrv, a.jscKey, a.jscDir = srv, key, dir
	return dir, srv.Env()
}

// stopChallengeSolver stops the solver, at shutdown.
func (a *App) stopChallengeSolver() {
	a.jscMu.Lock()
	defer a.jscMu.Unlock()
	if a.jscSrv != nil {
		a.jscSrv.Close()
		a.jscSrv = nil
	}
}
