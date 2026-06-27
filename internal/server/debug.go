package server

import (
	"expvar"
	"net/http"
	"net/http/pprof"
)

// mountDebug attaches the pprof + expvar profiling surface under /debug/, gated
// behind --debug. margin only ever binds loopback, so this is loopback-only by
// construction (http-server.md). Off by default; nothing is registered unless the
// operator opts in, so it adds zero surface to a normal serve.
func mountDebug(mux *http.ServeMux) {
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	mux.Handle("GET /debug/vars", expvar.Handler())
}
