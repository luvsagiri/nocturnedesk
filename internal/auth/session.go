package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const SessionCookieName = "nd_session"

var (
	ErrInvalidSession = errors.New("invalid session token")
	ErrExpiredSession = errors.New("session token expired")
)

func CreateSessionToken(userID int64, duration time.Duration, secretKey []byte) string {
	expiresAt := time.Now().Add(duration).Unix()
	payload := fmt.Sprintf("%d.%d", userID, expiresAt)

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s.%s", payload, sig)
}

func VerifySessionToken(token string, secretKey []byte) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, ErrInvalidSession
	}

	userIDStr, expiresStr, sig := parts[0], parts[1], parts[2]
	payload := fmt.Sprintf("%s.%s", userIDStr, expiresStr)

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return 0, ErrInvalidSession
	}

	expiresAt, err := strconv.ParseInt(expiresStr, 10, 64)
	if err != nil {
		return 0, ErrInvalidSession
	}

	if time.Now().Unix() > expiresAt {
		return 0, ErrExpiredSession
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return 0, ErrInvalidSession
	}

	return userID, nil
}

func SetSessionCookie(w http.ResponseWriter, token string, maxAgeSeconds int) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAgeSeconds,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // Allows HTTP on intranet
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
	})
}
