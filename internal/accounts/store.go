// Package accounts provides durable, email-verified accounts and revocable sessions.
// It stores neither passwords nor plaintext login/session secrets.
package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	ErrUnauthorized = errors.New("Bitte melde dich bei Zwoice an.")
	ErrDisabled     = errors.New("Dein Konto ist gesperrt. Bitte wende dich an den Betreiber.")
	ErrCode         = errors.New("Der Code ist ungültig oder abgelaufen. Bitte prüfe ihn oder fordere einen neuen an.")
	ErrRate         = errors.New("Bitte warte etwas, bevor du es erneut versuchst.")
	ErrMail         = errors.New("Die Anmeldung ist momentan nicht verfügbar. Bitte versuche es später erneut.")
	ErrTURN         = errors.New("TURN ist für dieses Konto deaktiviert oder das Tageslimit ist erreicht.")
)

type Mailer interface {
	SendLogin(context.Context, string, string, string) error
}
type Config struct {
	Path, AdminEmail string
	Mailer           Mailer
	DailyEmailLimit  int
	Now              func() time.Time
}
type Store struct {
	db  *bolt.DB
	cfg Config
}
type Account struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Created     int64  `json:"created"`
	LastLogin   int64  `json:"lastLogin"`
	Disabled    bool   `json:"disabled"`
	TURNAllowed bool   `json:"turnAllowed"`
	TURNLimit   int    `json:"turnLimit"`
	TURNDaily   int    `json:"turnDaily"`
	TURNDay     string `json:"turnDay"`
	Admin       bool   `json:"admin"`
}
type Session struct {
	AccountID        string `json:"accountId"`
	Expires, Created int64
}
type Challenge struct {
	Email, Digest string
	Expires       int64
	Attempts      int
}
type mailRate struct {
	Hour, Last int64
	Count      int
}
type Policy struct {
	RegistrationOpen bool `json:"registrationOpen"`
	TURNEnabled      bool `json:"turnEnabled"`
}
type Grant struct {
	Username, AccountID, SessionHash, ConnectionID string
	Expires                                        int64
	Revoke                                         bool
	NextAttempt                                    int64
}

var bucketNames = []string{"accounts", "emails", "sessions", "userSessions", "challenges", "mailRates", "meta", "grants"}

func Open(cfg Config) (*Store, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.DailyEmailLimit <= 0 {
		cfg.DailyEmailLimit = 90
	}
	if cfg.AdminEmail != "" {
		e, err := NormalizeEmail(cfg.AdminEmail)
		if err != nil {
			return nil, fmt.Errorf("invalid admin email")
		}
		cfg.AdminEmail = e
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(cfg.Path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, cfg: cfg}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, n := range bucketNames {
			if _, err := tx.CreateBucketIfNotExists([]byte(n)); err != nil {
				return err
			}
		}
		if b(tx, "meta").Get([]byte("policy")) == nil {
			return put(b(tx, "meta"), "policy", Policy{true, true})
		}
		return markGrants(tx, func(Grant) bool { return true })
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error              { return s.db.Close() }
func b(tx *bolt.Tx, n string) *bolt.Bucket { return tx.Bucket([]byte(n)) }
func put(bucket *bolt.Bucket, key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return bucket.Put([]byte(key), data)
}
func read(bucket *bolt.Bucket, key string, v any) error {
	data := bucket.Get([]byte(key))
	if data == nil {
		return ErrUnauthorized
	}
	return json.Unmarshal(data, v)
}
func secret() string {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}
func digest(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func NormalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	a, e := mail.ParseAddress(value)
	if e != nil || a.Address != value || len(value) > 254 || strings.ContainsAny(value, "\r\n") || !strings.Contains(strings.Split(value, "@")[len(strings.Split(value, "@"))-1], ".") {
		return "", errors.New("Bitte gib eine gültige E-Mail-Adresse ein.")
	}
	return value, nil
}
func (s *Store) Ready() bool { return s.cfg.Mailer != nil }
func (s *Store) Policy() (Policy, error) {
	var p Policy
	err := s.db.View(func(tx *bolt.Tx) error { return read(b(tx, "meta"), "policy", &p) })
	return p, err
}
func (s *Store) RequestCode(ctx context.Context, email string) (string, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	if !s.Ready() {
		return "", ErrMail
	}
	now := s.cfg.Now()
	id := secret()
	number, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%08d", number)
	deliver := true
	err = s.db.Update(func(tx *bolt.Tx) error {
		rates := b(tx, "mailRates")
		var rate mailRate
		if rates.Get([]byte(digest(email))) == nil && rates.Stats().KeyN >= 10000 {
			return ErrRate
		}
		_ = read(rates, digest(email), &rate)
		hour := now.Unix() / 3600
		if rate.Hour != hour {
			rate = mailRate{Hour: hour, Last: rate.Last}
		}
		if now.Unix()-rate.Last < 60 || rate.Count >= 3 {
			return ErrRate
		}
		var policy Policy
		if err := read(b(tx, "meta"), "policy", &policy); err != nil {
			return err
		}
		if uid := b(tx, "emails").Get([]byte(email)); uid != nil {
			var a Account
			if err := read(b(tx, "accounts"), string(uid), &a); err != nil {
				return err
			}
			if a.Disabled {
				deliver = false
			}
		}
		if b(tx, "emails").Get([]byte(email)) == nil && !policy.RegistrationOpen {
			deliver = false
		}
		if deliver {
			key := "emailDay:" + now.UTC().Format("2006-01-02")
			var count int
			_ = read(b(tx, "meta"), key, &count)
			if count >= s.cfg.DailyEmailLimit {
				return ErrRate
			}
			if err := put(b(tx, "meta"), key, count+1); err != nil {
				return err
			}
			if b(tx, "challenges").Stats().KeyN >= 10000 {
				return ErrRate
			}
			if err := put(b(tx, "challenges"), id, Challenge{Email: email, Digest: digest(id + ":" + code), Expires: now.Add(10 * time.Minute).Unix()}); err != nil {
				return err
			}
		}
		rate.Last = now.Unix()
		rate.Count++
		return put(rates, digest(email), rate)
	})
	if err != nil {
		return "", err
	}
	if deliver {
		if err := s.cfg.Mailer.SendLogin(ctx, email, code, id); err != nil {
			return "", ErrMail
		}
	}
	return id, nil // Same response for blocked accounts and closed registration.
}
func (s *Store) VerifyCode(id, code string, remember bool) (string, Account, int64, error) {
	var account Account
	var expiry int64
	if len(id) != 43 || len(code) != 8 {
		return "", account, 0, ErrCode
	}
	token := secret()
	hash := digest(token)
	now := s.cfg.Now()
	failure := error(nil)
	err := s.db.Update(func(tx *bolt.Tx) error {
		var ch Challenge
		if read(b(tx, "challenges"), id, &ch) != nil || ch.Expires <= now.Unix() || ch.Attempts >= 5 {
			failure = ErrCode
			return nil
		}
		ch.Attempts++
		expected := digest(id + ":" + code)
		if subtle.ConstantTimeCompare([]byte(ch.Digest), []byte(expected)) != 1 {
			failure = ErrCode
			return put(b(tx, "challenges"), id, ch)
		}
		if err := b(tx, "challenges").Delete([]byte(id)); err != nil {
			return err
		}
		uid := b(tx, "emails").Get([]byte(ch.Email))
		if uid == nil {
			var policy Policy
			if err := read(b(tx, "meta"), "policy", &policy); err != nil {
				return err
			}
			if !policy.RegistrationOpen {
				failure = ErrCode
				return nil
			}
			if b(tx, "accounts").Stats().KeyN >= 100000 {
				return ErrRate
			}
			account = Account{ID: secret(), Email: ch.Email, Created: now.Unix(), TURNAllowed: true, TURNLimit: 144}
			if err := b(tx, "emails").Put([]byte(ch.Email), []byte(account.ID)); err != nil {
				return err
			}
		} else if err := read(b(tx, "accounts"), string(uid), &account); err != nil {
			return err
		}
		if account.Disabled {
			failure = ErrDisabled
			return nil
		}
		account.LastLogin = now.Unix()
		if err := put(b(tx, "accounts"), account.ID, account); err != nil {
			return err
		}
		perUser, err := b(tx, "userSessions").CreateBucketIfNotExists([]byte(account.ID))
		if err != nil {
			return err
		}
		// Bound remembered devices and retire the oldest login, including its TURN grants.
		if perUser.Stats().KeyN >= 5 {
			var oldest string
			when := int64(1<<63 - 1)
			_ = perUser.ForEach(func(k, v []byte) error {
				var session Session
				if json.Unmarshal(v, &session) == nil && session.Created < when {
					oldest = string(k)
					when = session.Created
				}
				return nil
			})
			if oldest == "" {
				return errors.New("invalid session index")
			}
			if err := revokeSession(tx, oldest); err != nil {
				return err
			}
		}
		expiry = now.Add(12 * time.Hour).Unix()
		if remember {
			expiry = now.Add(30 * 24 * time.Hour).Unix()
		}
		session := Session{AccountID: account.ID, Expires: expiry, Created: now.Unix()}
		if err := put(b(tx, "sessions"), hash, session); err != nil {
			return err
		}
		return put(perUser, hash, session)
	})
	if err != nil {
		return "", Account{}, 0, err
	}
	if failure != nil {
		return "", Account{}, 0, failure
	}
	account.Admin = account.Email == s.cfg.AdminEmail
	return token, account, expiry, nil
}
func authorize(tx *bolt.Tx, token string, now int64) (Account, Session, error) {
	var a Account
	var session Session
	if len(token) != 43 {
		return a, session, ErrUnauthorized
	}
	if read(b(tx, "sessions"), digest(token), &session) != nil || session.Expires <= now {
		return a, session, ErrUnauthorized
	}
	if err := read(b(tx, "accounts"), session.AccountID, &a); err != nil {
		return a, session, err
	}
	if a.Disabled {
		return a, session, ErrDisabled
	}
	return a, session, nil
}
func (s *Store) Authenticate(token string) (Account, Session, error) {
	var a Account
	var session Session
	err := s.db.View(func(tx *bolt.Tx) error {
		var err error
		a, session, err = authorize(tx, token, s.cfg.Now().Unix())
		return err
	})
	a.Admin = a.Email != "" && a.Email == s.cfg.AdminEmail
	if a.TURNDay != s.cfg.Now().UTC().Format("2006-01-02") {
		a.TURNDaily = 0
	}
	return a, session, err
}
func revokeSession(tx *bolt.Tx, hash string) error {
	var session Session
	if read(b(tx, "sessions"), hash, &session) == nil {
		if index := b(tx, "userSessions").Bucket([]byte(session.AccountID)); index != nil {
			if err := index.Delete([]byte(hash)); err != nil {
				return err
			}
		}
	}
	if err := b(tx, "sessions").Delete([]byte(hash)); err != nil {
		return err
	}
	return markGrants(tx, func(g Grant) bool { return g.SessionHash == hash })
}
func (s *Store) Logout(token string) error {
	return s.db.Update(func(tx *bolt.Tx) error { return revokeSession(tx, digest(token)) })
}
func markGrants(tx *bolt.Tx, match func(Grant) bool) error {
	bucket := b(tx, "grants")
	cursor := bucket.Cursor()
	for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
		var grant Grant
		if json.Unmarshal(value, &grant) != nil {
			continue
		}
		if match(grant) && !grant.Revoke {
			grant.Revoke = true
			grant.NextAttempt = 0
			if err := put(bucket, string(key), grant); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Store) ReserveTURN(token string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		a, _, err := authorize(tx, token, s.cfg.Now().Unix())
		if err != nil {
			return err
		}
		var p Policy
		if err := read(b(tx, "meta"), "policy", &p); err != nil {
			return err
		}
		if !a.TURNAllowed || !p.TURNEnabled {
			return ErrTURN
		}
		day := s.cfg.Now().UTC().Format("2006-01-02")
		if a.TURNDay != day {
			a.TURNDay = day
			a.TURNDaily = 0
		}
		if a.TURNDaily >= a.TURNLimit {
			return ErrTURN
		}
		a.TURNDaily++
		return put(b(tx, "accounts"), a.ID, a)
	})
}
func (s *Store) RecordGrant(token, connectionID, username string, expiry int64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		a, _, err := authorize(tx, token, s.cfg.Now().Unix())
		if err != nil {
			return err
		}
		var p Policy
		if err := read(b(tx, "meta"), "policy", &p); err != nil {
			return err
		}
		if !a.TURNAllowed || !p.TURNEnabled {
			return ErrTURN
		}
		return put(b(tx, "grants"), username, Grant{Username: username, AccountID: a.ID, SessionHash: digest(token), ConnectionID: connectionID, Expires: expiry})
	})
}

// QueueRevocation retains credentials generated during a concurrent logout or ban.
func (s *Store) QueueRevocation(username string, expiry int64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return put(b(tx, "grants"), username, Grant{Username: username, Expires: expiry, Revoke: true})
	})
}
func (s *Store) RevokeConnection(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error { return markGrants(tx, func(g Grant) bool { return g.ConnectionID == id }) })
}
func (s *Store) PendingRevocations(limit int) ([]Grant, error) {
	out := []Grant{}
	now := s.cfg.Now().Unix()
	err := s.db.View(func(tx *bolt.Tx) error {
		c := b(tx, "grants").Cursor()
		for k, v := c.First(); k != nil && len(out) < limit; k, v = c.Next() {
			var g Grant
			if json.Unmarshal(v, &g) == nil && g.Revoke && g.NextAttempt <= now && g.Expires > now {
				out = append(out, g)
			}
		}
		return nil
	})
	return out, err
}
func (s *Store) RevocationResult(username string, success bool) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := b(tx, "grants")
		if success {
			return bucket.Delete([]byte(username))
		}
		var g Grant
		if read(bucket, username, &g) != nil {
			return nil
		}
		g.Revoke = true
		g.NextAttempt = s.cfg.Now().Add(time.Minute).Unix()
		return put(bucket, username, g)
	})
}
func (s *Store) List(token, cursor string) ([]Account, string, error) {
	actor, _, err := s.Authenticate(token)
	if err != nil || !actor.Admin {
		return nil, "", ErrUnauthorized
	}
	result := []Account{}
	next := ""
	err = s.db.View(func(tx *bolt.Tx) error {
		c := b(tx, "accounts").Cursor()
		k, v := c.First()
		if cursor != "" {
			k, v = c.Seek([]byte(cursor))
			if string(k) == cursor {
				k, v = c.Next()
			}
		}
		for ; k != nil; k, v = c.Next() {
			if len(result) == 100 {
				next = result[len(result)-1].ID
				break
			}
			var a Account
			if err := json.Unmarshal(v, &a); err != nil {
				return err
			}
			a.Admin = a.Email == s.cfg.AdminEmail
			if a.TURNDay != s.cfg.Now().UTC().Format("2006-01-02") {
				a.TURNDaily = 0
			}
			result = append(result, a)
		}
		return nil
	})
	return result, next, err
}
func (s *Store) SetAccess(token, id string, disabled, turnAllowed bool, limit int) error {
	if limit < 1 || limit > 10000 {
		return errors.New("Das Tageslimit muss zwischen 1 und 10.000 liegen.")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		actor, _, err := authorize(tx, token, s.cfg.Now().Unix())
		if err != nil || actor.Email != s.cfg.AdminEmail {
			return ErrUnauthorized
		}
		var a Account
		if err := read(b(tx, "accounts"), id, &a); err != nil {
			return err
		}
		if a.Email == s.cfg.AdminEmail && disabled {
			return errors.New("Das Betreiberkonto kann sich nicht selbst sperren.")
		}
		a.Disabled = disabled
		a.TURNAllowed = turnAllowed
		a.TURNLimit = limit
		if err := put(b(tx, "accounts"), id, a); err != nil {
			return err
		}
		if disabled {
			index := b(tx, "userSessions").Bucket([]byte(id))
			if index != nil {
				var hashes []string
				_ = index.ForEach(func(k, v []byte) error { hashes = append(hashes, string(k)); return nil })
				for _, h := range hashes {
					if err := revokeSession(tx, h); err != nil {
						return err
					}
				}
			}
		}
		if disabled || !turnAllowed {
			return markGrants(tx, func(g Grant) bool { return g.AccountID == id })
		}
		return nil
	})
}
func (s *Store) SetPolicy(token string, p Policy) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		a, _, err := authorize(tx, token, s.cfg.Now().Unix())
		if err != nil || a.Email != s.cfg.AdminEmail {
			return ErrUnauthorized
		}
		if err := put(b(tx, "meta"), "policy", p); err != nil {
			return err
		}
		if !p.TURNEnabled {
			return markGrants(tx, func(Grant) bool { return true })
		}
		return nil
	})
}
func (s *Store) Sweep() error {
	now := s.cfg.Now().Unix()
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"challenges", "grants", "sessions"} {
			bucket := b(tx, name)
			c := bucket.Cursor()
			var expired []string
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var item struct{ Expires int64 }
				if json.Unmarshal(v, &item) == nil && item.Expires <= now {
					expired = append(expired, string(k))
				}
			}
			for _, key := range expired {
				if name == "sessions" {
					if err := revokeSession(tx, key); err != nil {
						return err
					}
				} else if err := bucket.Delete([]byte(key)); err != nil {
					return err
				}
			}
		}
		rates := b(tx, "mailRates")
		c := rates.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var rate mailRate
			if json.Unmarshal(v, &rate) == nil && now-rate.Last > 24*3600 {
				if err := c.Delete(); err != nil {
					return err
				}
			}
		}
		meta := b(tx, "meta")
		c = meta.Cursor()
		day := "emailDay:" + s.cfg.Now().UTC().Format("2006-01-02")
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if strings.HasPrefix(string(k), "emailDay:") && string(k) != day {
				if err := c.Delete(); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
