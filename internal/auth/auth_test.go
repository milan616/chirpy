package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJWT_ValidToken(t *testing.T) {
	secret := "super-secret-key-12345"
	userID := uuid.New()
	duration := 1 * time.Hour

	// 1. Create token
	tokenString, err := MakeJWT(userID, secret, duration)
	if err != nil {
		t.Fatalf("expected no error creating token, got %v", err)
	}

	// 2. Validate token
	parsedID, err := ValidateJWT(tokenString, secret)
	if err != nil {
		t.Fatalf("expected no error validating token, got %v", err)
	}

	if parsedID != userID {
		t.Errorf("expected parsed user ID %s, got %s", userID, parsedID)
	}
}

func TestJWT_WrongSecret(t *testing.T) {
	correctSecret := "correct-secret"
	wrongSecret := "wrong-secret"
	userID := uuid.New()
	duration := 1 * time.Hour

	tokenString, err := MakeJWT(userID, correctSecret, duration)
	if err != nil {
		t.Fatalf("expected no error creating token, got %v", err)
	}

	// Validate with wrong secret should fail
	_, err = ValidateJWT(tokenString, wrongSecret)
	if err == nil {
		t.Error("expected error when validating with incorrect secret, got nil")
	}
}

func TestJWT_ExpiredToken(t *testing.T) {
	secret := "secret-key"
	userID := uuid.New()
	
	// Create a token that expires instantly (negative duration ensures it's expired immediately)
	duration := -1 * time.Second

	tokenString, err := MakeJWT(userID, secret, duration)
	if err != nil {
		t.Fatalf("expected no error creating token, got %v", err)
	}

	// Validate expired token should fail
	_, err = ValidateJWT(tokenString, secret)
	if err == nil {
		t.Error("expected error when validating an expired token, got nil")
	}
}
