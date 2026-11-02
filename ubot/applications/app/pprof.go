package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/pprof"
	"time"
)

// startPprof starts a standalone, internal-only profiling HTTP server
// bound strictly to 127.0.0.1:6060 using an isolated ServeMux.
// It gracefully shuts down when ctx is cancelled.
func startPprof(ctx context.Context) {
	mux := http.NewServeMux()

	// Register specific pprof handlers on the isolated mux
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))

	server := &http.Server{
		Addr:         "127.0.0.1:6060",
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		log.Println("🔍 [pprof] Internal profiling server listening on http://127.0.0.1:6060/debug/pprof/ (local only)")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("⚠️ [pprof] Could not start profiling server on 127.0.0.1:6060 (non-fatal): %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("⚠️ [pprof] Error during profiling server shutdown: %v", err)
		} else {
			log.Println("✅ [pprof] Profiling server stopped.")
		}
	}()
}

