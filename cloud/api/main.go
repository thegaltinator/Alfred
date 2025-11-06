package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"alfred-cloud/streams"
)

type HealthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Service string `json:"service"`
}

const VERSION = "0.0.1"

var productivityStream string

func main() {
	ctx := context.Background()
	redisClient, err := streams.Init(ctx)
	if err != nil {
		log.Fatalf("failed to initialize redis: %v", err)
	}
	defer redisClient.Close()

	log.Printf("Redis connected (%s)", redisClient.Options().Addr)

	userID := strings.TrimSpace(os.Getenv("ALFRED_USER_ID"))
	if userID == "" {
		userID = "dev:test"
	}
	productivityStream = fmt.Sprintf("user:%s:in:productivity", userID)

	log.Printf("Productivity stream: %s", productivityStream)

	r := mux.NewRouter()

	// Health check endpoint
	r.HandleFunc("/healthz", healthHandler).Methods("GET")
	r.HandleFunc("/", rootHandler).Methods("GET")
	r.HandleFunc("/prod/heartbeat", heartbeatHandler).Methods("POST")

	// Configure server
	srv := &http.Server{
		Handler:      r,
		Addr:         "0.0.0.0:8000",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Printf("Alfred Cloud Server v%s starting on %s", VERSION, srv.Addr)
	log.Fatal(srv.ListenAndServe())
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := HealthResponse{
		OK:      true,
		Version: VERSION,
		Service: "alfred-cloud",
	}

	json.NewEncoder(w).Encode(response)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]string{
		"message": "Alfred Cloud API Server",
		"version": VERSION,
		"docs":    "/docs",
	}

	json.NewEncoder(w).Encode(response)
}
