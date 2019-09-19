package debug

import (
	"net/http/pprof"

	"github.com/gorilla/mux"
)

// Handler is the HTTP handler used to handle debug operations.
type Handler struct {
	*mux.Router
}

// NewHandler returns a pointer to an Handler
func NewHandler() *Handler {
	h := &Handler{
		Router: mux.NewRouter(),
	}

	h.HandleFunc("/debug/pprof/", pprof.Index)
	h.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	h.HandleFunc("/debug/pprof/profile", pprof.Profile)
	h.HandleFunc("/debug/pprof/symbol", pprof.Symbol)

	h.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	h.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	h.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
	h.Handle("/debug/pprof/block", pprof.Handler("block"))

	return h
}
