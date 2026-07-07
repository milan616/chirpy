package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

// 1. Create the apiConfig struct to hold stateful metrics safely
type apiConfig struct {
	fileserverHits atomic.Int32
}

// 2. Middleware to increment fileserverHits counter
func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1) // Safely add 1 to the counter
		next.ServeHTTP(w, r)       // Pass control to the next handler
	})
}

// 3. Handler to view metrics at /metrics
func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// Use Load() to safely read the atomic value
	w.Write([]byte(fmt.Sprintf("Hits: %d", cfg.fileserverHits.Load())))
}

// 4. Handler to reset metrics at /reset
func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// Use Store() to safely reset the value to 0
	cfg.fileserverHits.Store(0)
	w.Write([]byte("Hits reset to 0"))
}

func main() {
	mux := http.NewServeMux()

	// Initialize our state tracking instance
	apiCfg := &apiConfig{}

	// Wrap the basic fileserver with our new tracking middleware
	fileServerHandler := http.FileServer(http.Dir("."))
	wrappedHandler := apiCfg.middlewareMetricsInc(http.StripPrefix("/app", fileServerHandler))
	mux.Handle("/app/", wrappedHandler)

	// Non-tracked basic endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Register methods attached to our apiCfg instance
	mux.HandleFunc("/metrics", apiCfg.handlerMetrics)
	mux.HandleFunc("/reset", apiCfg.handlerReset)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Starting server on port 8080...")
	err := server.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}
