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
	"github.com/milan616/chirpy/internal/auth"

	_ "github.com/lib/pq"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// 1. Create the apiConfig struct to hold stateful metrics safely
type apiConfig struct {
	fileserverHits	atomic.Int32
	db 				*database.Queries
	platform		string
	jwtSecret		string
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

type LoginUser struct {
	Password			string	`json:"password"`
	Email				string	`json:"email"`
	ExpiresInSeconds	int		`json:"expires_in_seconds"`
}

type Chirp struct {
	ID			uuid.UUID	`json:"id"`
	CreatedAt	time.Time	`json:"created_at"`
	UpdatedAt	time.Time	`json:"updated_at"`
	Body		string 		`json:"body"`
	UserID		uuid.UUID	`json:"user_id"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) handlerMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	adminText := `<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`
	
	w.Write([]byte(fmt.Sprintf(adminText, cfg.fileserverHits.Load())))
}

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
	// 1. Authenticate Request via Bearer JWT first
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 2. Unpack Chirp parameters
	decoder := json.NewDecoder(r.Body)
	var req chirpRequest
	err = decoder.Decode(&req)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if len(req.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	cleaned := naughtyCleaner(req.Body)

	chirpParams := database.CreateChirpParams{
		ID:        uuid.New(),
		CreatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		UpdatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		Body:      cleaned,
		UserID:    userID, // Controlled by JWT
	}

	ctx := context.Background()
	newChirp, err := cfg.db.CreateChirp(ctx, chirpParams)
	if err != nil {
		log.Printf("Database error creating chirp: %v", err)
		respondWithError(w, http.StatusBadRequest, "Could not create new chirp")
		return
	}

	respondWithJSON(w, http.StatusCreated, Chirp{
		ID:        newChirp.ID,
		CreatedAt: newChirp.CreatedAt.Time,
		UpdatedAt: newChirp.UpdatedAt.Time,
		Body:      newChirp.Body,
		UserID:    newChirp.UserID,
	})
}

func (cfg *apiConfig) handlerCreateUser(w http.ResponseWriter, r *http.Request) {
	
	decoder := json.NewDecoder(r.Body)
	var user LoginUser
	err := decoder.Decode(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if user.Email == "" || user.Password == "" {
		respondWithError(w, http.StatusBadRequest, "Missing required email/password")
		return
	}

	var hashedPass string
	hashedPass, err = auth.HashPassword(user.Password)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to validate password")
	}

	newUserParams := database.CreateUserParams {
		ID: uuid.New(),
		CreatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		UpdatedAt: sql.NullTime{Time: time.Now(), Valid: true},
		Email:      user.Email,
		HashedPassword:	hashedPass,
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
		CreatedAt: newUser.CreatedAt.Time,
		UpdatedAt: newUser.UpdatedAt.Time,
	})
}

func (cfg *apiConfig) handlerLogin(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var loginUser LoginUser
	err := decoder.Decode(&loginUser)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if loginUser.Email == "" || loginUser.Password == "" {
		respondWithError(w, http.StatusBadRequest, "Missing required email/password")
		return
	}

	ctx := context.Background()
	matchUser, err := cfg.db.GetUserByEmail(ctx, loginUser.Email)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}

	match := false
	match, err = auth.CheckPasswordHash(loginUser.Password, matchUser.HashedPassword)
	if err != nil || !match {
		respondWithError(w, http.StatusUnauthorized, "Incorrect email or password")
		return
	}

	// Default expiration handling
	defaultExpiry := 1 * time.Hour
	if loginUser.ExpiresInSeconds > 0 {
		userExpiry := time.Duration(loginUser.ExpiresInSeconds) * time.Second
		// Cap it to 1 hour maximum
		if userExpiry < defaultExpiry {
			defaultExpiry = userExpiry
		}
	}

	token, err := auth.MakeJWT(matchUser.ID, cfg.jwtSecret, defaultExpiry)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not generate authentication token")
		return
	}

	// Custom inline shape matching assignment requirements
	respondWithJSON(w, http.StatusOK, struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
		Token     string    `json:"token"`
	}{
		ID:        matchUser.ID,
		Email:     matchUser.Email,
		CreatedAt: matchUser.CreatedAt.Time,
		UpdatedAt: matchUser.UpdatedAt.Time,
		Token:     token,
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
	apiCfg.jwtSecret = os.Getenv("JWT_SECRET")

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
	mux.HandleFunc("POST /api/login", apiCfg.handlerLogin)
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
