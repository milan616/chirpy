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
	polkaKey		string
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
	IsChirpyRed	bool	  `json:"is_chirpy_red"`
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
		IsChirpyRed: newUser.IsChirpyRed.Bool,
	})
}

func (cfg *apiConfig) handlerUpdateUser(w http.ResponseWriter, r *http.Request) {

	bearer, err := auth.GetBearerToken(r.Header)
	var userID uuid.UUID
	userID, err = auth.ValidateJWT(bearer, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	
	decoder := json.NewDecoder(r.Body)
	var updateUser LoginUser
	err = decoder.Decode(&updateUser)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	}

	if updateUser.Email == "" || updateUser.Password == "" {
		respondWithError(w, http.StatusBadRequest, "Missing required email/password")
		return
	}

	var curUser database.User
	ctx := context.Background()
	curUser, err = cfg.db.GetUserByID(ctx, userID)
	if err != nil || userID != curUser.ID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var hashedPass string
	hashedPass, err = auth.HashPassword(updateUser.Password)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to validate password")
		return
	}

	updParams := database.UpdateUserInfoParams{
		Email:			curUser.Email,
		Email_2:		updateUser.Email,
		HashedPassword:	hashedPass,
		UpdatedAt:		sql.NullTime{Time: time.Now(), Valid: true},
	}

	err = cfg.db.UpdateUserInfo(ctx, updParams)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to update user info")
		return
	}

	curUser, err = cfg.db.GetUserByEmail(ctx, updateUser.Email)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
	}
	
	respondWithJSON(w, http.StatusOK, User{
		ID:        curUser.ID,
		Email:     curUser.Email,
		CreatedAt: curUser.CreatedAt.Time,
		UpdatedAt: curUser.UpdatedAt.Time,
		IsChirpyRed: curUser.IsChirpyRed.Bool,
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

	tokenExpiry := 1 * time.Hour

	token, err := auth.MakeJWT(matchUser.ID, cfg.jwtSecret, tokenExpiry)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not generate authentication token")
		return
	}

	refreshToken := auth.MakeRefreshToken()
	refreshDuration := 60 * 24 * time.Hour
	refreshExpiryTimestamp := time.Now().Add(refreshDuration)

	params := database.CreateRefreshTokenParams{
		Token:		refreshToken,
		CreatedAt:	sql.NullTime{Time: time.Now(), Valid: true},
		UpdatedAt:	sql.NullTime{Time: time.Now(), Valid: true},
		UserID:		matchUser.ID,
		ExpiresAt:	sql.NullTime{Time: refreshExpiryTimestamp, Valid: true},
		RevokedAt:	sql.NullTime{Time: time.Time{}, Valid: false},
	}

	_, err = cfg.db.CreateRefreshToken(ctx, params)

	respondWithJSON(w, http.StatusOK, struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
		Token     string    `json:"token"`
		RefreshToken	string	`json:"refresh_token"`
		IsChirpyRed		bool	`json:"is_chirpy_red"`
	}{
		ID:        matchUser.ID,
		Email:     matchUser.Email,
		CreatedAt: matchUser.CreatedAt.Time,
		UpdatedAt: matchUser.UpdatedAt.Time,
		Token:     token,
		RefreshToken: refreshToken,
		IsChirpyRed: matchUser.IsChirpyRed.Bool,
	})
}

func (cfg *apiConfig) handlerRefresh(w http.ResponseWriter, r *http.Request) {
	tokenStr, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Missing or invalid token format")
		return
	}

	ctx := context.Background()
	dbToken, err := cfg.db.GetRefreshToken(ctx, tokenStr)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if dbToken.ExpiresAt.Time.Before(time.Now()) || dbToken.RevokedAt.Valid {
		respondWithError(w, http.StatusUnauthorized, "Token expired or revoked")
		return
	}

	user, err := cfg.db.GetUserFromRefreshToken(ctx, tokenStr)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	newAccessToken, err := auth.MakeJWT(user.ID, cfg.jwtSecret, 1*time.Hour)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not generate access token")
		return
	}

	respondWithJSON(w, http.StatusOK, struct {
		Token string `json:"token"`
	}{
		Token: newAccessToken,
	})
}

func (cfg *apiConfig) handlerRevoke(w http.ResponseWriter, r *http.Request) {
	tokenStr, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Missing or invalid token format")
		return
	}

	ctx := context.Background()
	err = cfg.db.RevokeRefreshToken(ctx, database.RevokeRefreshTokenParams{
		Token:     tokenStr,
		RevokedAt: sql.NullTime{Time: time.Now(), Valid: true},
		UpdatedAt: sql.NullTime{Time: time.Now(), Valid: true},
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not revoke token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

func (cfg *apiConfig) handlerDeleteChirpByID(w http.ResponseWriter, r *http.Request) {
	chirpIDString := r.PathValue("chirpID")

	chirpUUID, err := uuid.Parse(chirpIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid chirp ID format")
		return
	}

	bearer, err := auth.GetBearerToken(r.Header)
	var userID uuid.UUID
	userID, err = auth.ValidateJWT(bearer, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	ctx := context.Background()
	var chirp database.Chirp
	chirp, err = cfg.db.GetChirp(ctx, chirpUUID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Could not find chirp")
		return
	} else if chirp.UserID != userID {
		respondWithError(w, http.StatusForbidden, "Forbidden")
		return
	}

	err = cfg.db.DeleteChirp(ctx, chirp.ID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to delete chirp")
	}

	respondWithJSON(w, http.StatusNoContent, "Chirp deleted")
}

func (cfg *apiConfig) handlerChirpyRedUpgrade(w http.ResponseWriter, r *http.Request) {
	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil || cfg.polkaKey != apiKey {
		respondWithError(w, http.StatusUnauthorized, "Invalid API Key")
		return
	}
	
	type RedUpgrade struct {
		Event string `json:"event"`
		Data  struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}

	decoder := json.NewDecoder(r.Body)
	var redUpgrade RedUpgrade
	err = decoder.Decode(&redUpgrade)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Something went wrong")
		return
	} else if redUpgrade.Event != "user.upgraded" {
		respondWithError(w, http.StatusNoContent, "")
		return
	}

	var userID uuid.UUID
	userID, err = uuid.Parse(redUpgrade.Data.UserID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Malformed user_id")
		return
	}

	params := database.ChirpyRedUpgradeParams{
		ID:			userID,
		UpdatedAt:	sql.NullTime{Time: time.Now(), Valid: true},
	}

	ctx := context.Background()
	err = cfg.db.ChirpyRedUpgrade(ctx, params)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "")
		return
	}

	respondWithJSON(w, http.StatusNoContent, "")
	
}

func naughtyCleaner(chirp string) string {
	naughty := []string{"kerfuffle", "sharbert", "fornax"}

	words := strings.Split(chirp, " ")

	for i, word := range words {
		cleanedWord := strings.ToLower(word)
		
		if slices.Contains(naughty, cleanedWord) {
			words[i] = "****"
		}
	}

	return strings.Join(words, " ")
}

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
	apiCfg.polkaKey = os.Getenv("POLKA_KEY")

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
	mux.HandleFunc("PUT /api/users", apiCfg.handlerUpdateUser)
	mux.HandleFunc("POST /api/login", apiCfg.handlerLogin)
	mux.HandleFunc("POST /api/refresh", apiCfg.handlerRefresh)
	mux.HandleFunc("POST /api/revoke", apiCfg.handlerRevoke)
	
	mux.HandleFunc("POST /api/chirps", apiCfg.handlerCreateChirp)
	mux.HandleFunc("GET /api/chirps", apiCfg.handlerGetChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.handlerGetChirpByID)
	mux.HandleFunc("DELETE /api/chirps/{chirpID}", apiCfg.handlerDeleteChirpByID)

	mux.HandleFunc("POST /api/polka/webhooks", apiCfg.handlerChirpyRedUpgrade)

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
