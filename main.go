package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	// 1. Setup the File Server and wrap it with StripPrefix
	// This maps requests to localhost:8080/app/* to files in your root directory .
	fileServerHandler := http.FileServer(http.Dir("."))
	mux.Handle("/app/", http.StripPrefix("/app", fileServerHandler))

	// 2. Register the readiness endpoint using HandleFunc
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// Set headers first
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// Set status code
		w.WriteHeader(http.StatusOK) // http.StatusOK is just the constant for 200
		// Write body
		w.Write([]byte("OK"))
	})

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
