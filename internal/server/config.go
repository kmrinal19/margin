package server

// Config holds the settings for a running server. All fields are required;
// the CLI supplies defaults.
type Config struct {
	Host    string // bind address; defaults to 127.0.0.1 (offline-only)
	Port    int    // listen port
	DocsDir string // directory of authored Markdown docs
	DataDir string // directory holding the SQLite comment store
	Debug   bool   // expose pprof/expvar on loopback when true
}
