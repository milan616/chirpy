package auth

import (
	"fmt"
	"time"
	"strings"
	"net/http"
	"crypto/rand"
	"encoding/hex"
	
	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
	"github.com/golang-jwt/jwt/v5"
)

func HashPassword(password string) (string, error) {

	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return "", err
	}

	return hash, nil
}

func CheckPasswordHash(password, hash string) (bool, error) {

	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, err
	}

	return match, nil
}

func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {

	claims := &jwt.RegisteredClaims{
		Issuer:		"chirpy-access",
		IssuedAt:	jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt:	jwt.NewNumericDate(time.Now().UTC().Add(expiresIn)),
		Subject:	userID.String(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := token.SignedString([]byte(tokenSecret))

	return ss, err
}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	claimsStruct := &jwt.RegisteredClaims{}

	token, err := jwt.ParseWithClaims(
		tokenString,
		claimsStruct,
		func(token *jwt.Token) (interface{}, error) {
			return []byte(tokenSecret), nil
		},
	)
	if err != nil {
		return uuid.Nil, err
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return uuid.Nil, fmt.Errorf("unknown claims type format encountered")
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid user ID layout inside subject field")
	}

	return userID, nil
}

func GetBearerToken(headers http.Header) (string, error) {
	authHeader := headers.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header")
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", fmt.Errorf("invalid authorization header format")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("missing token value")
	}

	return token, nil
}

func GetAPIKey(headers http.Header) (string, error) {
	authHeader := headers.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header")
	}

	if !strings.HasPrefix(authHeader, "ApiKey ") {
		return "", fmt.Errorf("invalid authorization header format")
	}

	token := strings.TrimPrefix(authHeader, "ApiKey ")
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("missing token value")
	}

	return token, nil
}

func MakeRefreshToken() string {
	key := make([]byte, 32)
	rand.Read(key)

	return hex.EncodeToString(key)
}
