package main

import (
	"encoding/json"
	"net/http"

	"alfred-cloud/subagents/productivity"
	"github.com/gorilla/mux"
)

func registerProdDebugRoutes(r *mux.Router, heuristics *productivity.HeuristicService) {
	r.HandleFunc("/debug/prod/heuristics", func(w http.ResponseWriter, req *http.Request) {
		userID := req.URL.Query().Get("user_id")
		if userID == "" {
			http.Error(w, "user_id required", http.StatusBadRequest)
			return
		}
		list, err := heuristics.ListHeuristics(req.Context(), userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_id":    userID,
			"heuristics": list,
			"count":      len(list),
		})
	}).Methods("GET")
}
