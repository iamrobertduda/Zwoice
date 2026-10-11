package auth

import (
	"strings"
	"testing"
	"time"
)

func TestInvitation(t *testing.T) {
	key := []byte(strings.Repeat("a", 32))
	now := time.Unix(10000, 0)
	claims := Claims{AthleteID: 42, Cohort: "friends", Expires: now.Add(time.Hour).Unix()}
	token, err := Sign(key, claims)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(key, token, now)
	if err != nil || got != claims {
		t.Fatalf("round trip: %v %v", got, err)
	}
	for _, tc := range []struct {
		name, token string
		key         []byte
		now         time.Time
	}{
		{"tampered", token[:10] + "x" + token[11:], key, now},
		{"wrong key", token, []byte(strings.Repeat("b", 32)), now},
		{"expired", token, key, now.Add(time.Hour)},
		{"oversized", strings.Repeat("x", 1025), key, now},
		{"malformed", "eyJ9.not-base64", key, now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Verify(tc.key, tc.token, tc.now); err == nil {
				t.Fatal("accepted invalid token")
			}
		})
	}
}
