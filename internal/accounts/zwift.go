package accounts

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

var ErrZwiftLink = errors.New("Bitte verknüpfe zuerst deinen Zwift-Fahrer.")
var ErrZwiftClaimed = errors.New("Dieser Fahrer ist bereits mit einem anderen Zwoice-Konto verknüpft.")
var ErrZwiftProof = errors.New("Der Bestätigungscode ist noch nicht im Zwift-Namen sichtbar oder bereits abgelaufen.")

type ZwiftLink struct {
	AthleteID  int64  `json:"athleteId"`
	Name       string `json:"name"`
	VerifiedAt int64  `json:"verifiedAt"`
}

type ZwiftChallenge struct {
	AthleteID int64  `json:"athleteId"`
	Code      string `json:"code"`
	Expires   int64  `json:"expires"`
	Created   int64  `json:"created"`
}

// BeginZwiftLink never accepts a profile supplied by the client as proof. The
// broker must retrieve a fresh profile through its authenticated observer.
func (s *Store) BeginZwiftLink(token string, athleteID int64) (ZwiftChallenge, error) {
	var challenge ZwiftChallenge
	if athleteID <= 0 || athleteID > 9007199254740991 {
		return challenge, ErrZwiftLink
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		now := s.cfg.Now().Unix()
		a, _, err := authorize(tx, token, now)
		if err != nil {
			return err
		}
		owner := b(tx, "zwiftOwners").Get([]byte(strconv.FormatInt(athleteID, 10)))
		if owner != nil && string(owner) != a.ID {
			return ErrZwiftClaimed
		}
		var old ZwiftChallenge
		if read(b(tx, "zwiftChallenges"), a.ID, &old) == nil && old.Expires > now {
			if old.AthleteID == athleteID {
				challenge = old
				return nil
			}
			if now-old.Created < 30 {
				return ErrRate
			}
		}
		var random [5]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		challenge = ZwiftChallenge{AthleteID: athleteID, Code: "ZV-" + strings.ToUpper(hex.EncodeToString(random[:])), Created: now, Expires: now + int64((15*time.Minute)/time.Second)}
		return put(b(tx, "zwiftChallenges"), a.ID, challenge)
	})
	return challenge, err
}

func (s *Store) ZwiftChallenge(token string) (ZwiftChallenge, error) {
	var challenge ZwiftChallenge
	err := s.db.View(func(tx *bolt.Tx) error {
		a, _, err := authorize(tx, token, s.cfg.Now().Unix())
		if err != nil {
			return err
		}
		if read(b(tx, "zwiftChallenges"), a.ID, &challenge) != nil || challenge.Expires <= s.cfg.Now().Unix() {
			return ErrZwiftProof
		}
		return nil
	})
	return challenge, err
}

// ConfirmZwiftLink consumes the exact challenge observed in an independently
// fetched profile. Comparing Code also prevents in-flight confirmations from
// committing a subsequently replaced challenge.
func (s *Store) ConfirmZwiftLink(token string, expected ZwiftChallenge, profileID int64, profileName string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		now := s.cfg.Now().Unix()
		a, _, err := authorize(tx, token, now)
		if err != nil {
			return err
		}
		var ch ZwiftChallenge
		if read(b(tx, "zwiftChallenges"), a.ID, &ch) != nil || ch.Expires <= now || ch.Code != expected.Code || ch.AthleteID != profileID {
			return ErrZwiftProof
		}
		found := false
		for _, word := range strings.Fields(profileName) {
			if word == ch.Code {
				found = true
			}
		}
		if !found {
			return ErrZwiftProof
		}
		key := strconv.FormatInt(profileID, 10)
		owners := b(tx, "zwiftOwners")
		if owner := owners.Get([]byte(key)); owner != nil && string(owner) != a.ID {
			return ErrZwiftClaimed
		}
		if a.Zwift != nil {
			if err := owners.Delete([]byte(strconv.FormatInt(a.Zwift.AthleteID, 10))); err != nil {
				return err
			}
		}
		a.Zwift = &ZwiftLink{AthleteID: profileID, Name: strings.Join(strings.Fields(strings.ReplaceAll(profileName, ch.Code, "")), " "), VerifiedAt: now}
		if err := put(b(tx, "accounts"), a.ID, a); err != nil {
			return err
		}
		if err := owners.Put([]byte(key), []byte(a.ID)); err != nil {
			return err
		}
		if err := b(tx, "zwiftChallenges").Delete([]byte(a.ID)); err != nil {
			return err
		}
		return markGrants(tx, func(g Grant) bool { return g.AccountID == a.ID })
	})
}

func (s *Store) UnlinkZwift(token string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		a, _, err := authorize(tx, token, s.cfg.Now().Unix())
		if err != nil {
			return err
		}
		if a.Zwift != nil {
			if err := b(tx, "zwiftOwners").Delete([]byte(strconv.FormatInt(a.Zwift.AthleteID, 10))); err != nil {
				return err
			}
			a.Zwift = nil
			if err := put(b(tx, "accounts"), a.ID, a); err != nil {
				return err
			}
		}
		if err := b(tx, "zwiftChallenges").Delete([]byte(a.ID)); err != nil {
			return err
		}
		return markGrants(tx, func(g Grant) bool { return g.AccountID == a.ID })
	})
}
