package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"encoding/json"
	"strings"
	"slices"
	"database/sql"
	"os"
	"time"
	"context"

	"github.com/milan616/chirpy/internal/database"

	_ "github.com/lib/pq"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// 1. Create the apiConfig struct to hold stateful metrics safely
type apiConfig struct {
	fileserverHits	atomic.Int32
	db 				*database.Queries
	platform		string
}

type chirpRequest struct {
	Body string `json:"body"`
	UserID string `json:"user_id"`
}

type User struct {
	ID			uuid.UUID `json:"id"`
	CreatedAt	time.Time `json:"created_at"`
	UpdatedAt	time.Time `json:"updated_at"`
	Email		string    `json:"email"`
}

type Chirp struct {
	ID			uuid.UUID	`json:"id"`
	CreatedAt	time.Time	`json:"created_at"`
	UpdatedAt	time.Time	`json:"updated_at"`
	Body		string 		`json:"body"`
	UserID		uuid.UUID	`json:"user_id"`
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	adminText := `<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`
	
	// Use Load() to safely read the atomic value
	w.Write([]byte(fmt.Sprintf(adminText, cfg.fileserverHits.Load())))
}

// 4. Handler to reset metrics at /reset
func (cfg *apiConfig) handlerReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if cfg.platform != "DEV" {
		w.WriteHeader(http.StatusForbidden)
	} else {
		w.WriteHeader(http.StatusOK)
		ctx := context.Background()
		cfg.db.ResetUsers(ctx)
		cfg.fileserverHits.Store(0)
		w.Write([]byte("Hits reset to 0"))
	}
}

func (cfg *apiConfig) handlerCreateChirp(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var req chirpRequest
	err := decoder.Decode(&req)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if len(req.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleaned := naughtyCleaner(req.Body)
	var userUUID uuid.UUID
	userUUID, err = uuid.Parse(req.UserID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid user_id")
	}

	chirpParams := database.CreateChirpParams {
		ID: uuid.New(),
		CreatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		UpdatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		Body:      cleaned,
		UserID:    userUUID,
	}

	ctx := context.Background()
	var newChirp database.Chirp

	newChirp, err = cfg.db.CreateChirp(ctx, chirpParams)
	if err != nil {
		log.Printf("Database error creating chirp: %v", err)
		respondWithError(w, http.StatusBadRequest, "Could not create new chirp")
		return
	}

	respondWithJSON(w, http.StatusCreated, Chirp{
		ID:			newChirp.ID,
		CreatedAt:	newChirp.CreatedAt.Time,
		UpdatedAt:	newChirp.UpdatedAt.Time,
		Body:		newChirp.Body,
		UserID:		newChirp.UserID,
	})
}

func (cfg *apiConfig) handlerCreateUser(w http.ResponseWriter, r *http.Request) {
	type NewUser struct {
		Email	string	`json:"email"`
	}
	
	decoder := json.NewDecoder(r.Body)
	var email NewUser
	err := decoder.Decode(&email)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if email.Email == "" {
		respondWithError(w, http.StatusBadRequest, "Missing email field")
		return
	}

	newUserParams := database.CreateUserParams {
		ID: uuid.New(),
		CreatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		UpdatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		Email:      email.Email,
	}

	ctx := context.Background()
	var newUser database.User
	newUser, err = cfg.db.CreateUser(ctx, newUserParams)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Could not create new user")
		return
	}
	
	respondWithJSON(w, http.StatusCreated, User{
		ID:        newUser.ID,
		Email:     newUser.Email,
		CreatedAt: newUser.CreatedAt.Time, // Unpacks the real time from sql.NullTime
		UpdatedAt: newUser.UpdatedAt.Time, // Unpacks the real time from sql.NullTime
	})
}

func (cfg *apiConfig) handlerGetChirps(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	dbChirps, err := cfg.db.GetAllChirps(ctx)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Could not retrieve chirps")
	}

	apiChirps := make([]Chirp, len(dbChirps))

	for i, dbChirp := range dbChirps {
		apiChirps[i] = Chirp{
			ID:        dbChirp.ID,
			CreatedAt: dbChirp.CreatedAt.Time,
			UpdatedAt: dbChirp.UpdatedAt.Time,
			Body:      dbChirp.Body,
			UserID:    dbChirp.UserID,
		}
	}

	respondWithJSON(w, http.StatusOK, apiChirps)
}

func (cfg *apiConfig) handlerGetChirpByID(w http.ResponseWriter, r *http.Request) {
	chirpIDString := r.PathValue("chirpID")

	chirpUUID, err := uuid.Parse(chirpIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID format")
		return
	}

	ctx := context.Background()
	dbChirp, err := cfg.db.GetChirp(ctx, chirpUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusNotFound, "Chirp not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "Could not retrieve chirp")
		return
	}

	respondWithJSON(w, http.StatusOK, Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt.Time,
		UpdatedAt: dbChirp.UpdatedAt.Time,
		Body:      dbChirp.Body,
		UserID:    dbChirp.UserID,
	})
}

func naughtyCleaner(chirp string) string {
	// Let's use a slice instead of an array so it works directly with slices.Contains
	naughty := []string{"kerfuffle", "sharbert", "fornax"}

	// Split the original chirp so we can modify individual words while keeping casing
	words := strings.Split(chirp, " ")

	for i, word := range words {
		// Clean up the word for a pure lowercase comparison check
		cleanedWord := strings.ToLower(word)
		
		// The Python equivalent of: if cleanedWord in naughty:
		if slices.Contains(naughty, cleanedWord) {
			words[i] = "****"
		}
	}

	// Rejoin the slice back into a full string space-separated
	return strings.Join(words, " ")
}

// Helper to respond with clean JSON payloads
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

// Helper to standardise error responses using the JSON helper
func respondWithError(w http.ResponseWriter, code int, msg string) {
	type errorResponse struct {
		Error string `json:"error"`
	}
	respondWithJSON(w, code, errorResponse{
		Error: msg,
	})
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	dbQueries := database.New(db)
	
	mux := http.NewServeMux()

	// Initialize our state tracking instance
	apiCfg := &apiConfig{}
	apiCfg.db = dbQueries
	apiCfg.platform = os.Getenv("PLATFORM")

	// Wrap the basic fileserver with our new tracking middleware
	fileServerHandler := http.FileServer(http.Dir("."))
	wrappedHandler := apiCfg.middlewareMetricsInc(http.StripPrefix("/app", fileServerHandler))
	mux.Handle("/app/", wrappedHandler)

	// Non-tracked basic endpoint
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("POST /api/users", apiCfg.handlerCreateUser)
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerCreateChirp)
	mux.HandleFunc("GET /api/chirps", apiCfg.handlerGetChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.handlerGetChirpByID)

	// Register methods attached to our apiCfg instance
	mux.HandleFunc("GET /admin/metrics", apiCfg.handlerMetrics)
	mux.HandleFunc("POST /admin/reset", apiCfg.handlerReset)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Starting server on port 8080...")
	err = server.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}
