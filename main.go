package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	// 1. Convert the current directory string "." into an http.Dir type
	directory := http.Dir(".")

	// 2. Pass it to http.FileServer to create a file-serving handler
	fileServerHandler := http.FileServer(directory)

	// 3. Register it with the ServeMux for the root "/" path
	mux.Handle("/", fileServerHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Starting file server on port 8080...")
	err := server.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}
