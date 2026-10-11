// Package auth provides expiring, athlete-bound invitations for operator-managed membership.
// It does not claim to verify ownership of a Zwift account.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Claims struct {
	AthleteID int64  `json:"athleteId"`
	Cohort    string `json:"cohort"`
	Expires   int64  `json:"expires"`
}

func Sign(key []byte, claims Claims) (string, error) {
	if len(key) < 32 || claims.AthleteID <= 0 || len(claims.Cohort) == 0 || len(claims.Cohort) > 64 {
		return "", errors.New("key must contain at least 32 bytes; athlete and cohort are required")
	}
	data, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func Verify(key []byte, token string, now time.Time) (Claims, error) {
	var claims Claims
	if len(key) < 32 || len(token) > 1024 {
		return claims, errors.New("invalid invitation")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return claims, errors.New("invalid invitation")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0]))
	if err != nil || !hmac.Equal(mac.Sum(nil), sig) {
		return claims, errors.New("invalid invitation")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(data, &claims) != nil || claims.AthleteID <= 0 || len(claims.Cohort) == 0 || len(claims.Cohort) > 64 || claims.Expires <= now.Unix() {
		return Claims{}, errors.New("invalid or expired invitation")
	}
	return claims, nil
}
