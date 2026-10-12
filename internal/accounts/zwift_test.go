package accounts

import (
	"errors"
	"testing"
	"time"
)

func TestZwiftOwnershipAndUnlink(t *testing.T) {
	s, m, _ := fixture(t)
	first, _ := login(t, s, m, "first@example.test", true)
	second, _ := login(t, s, m, "second@example.test", true)
	ch, err := s.BeginZwiftLink(first, 123)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmZwiftLink(second, ch, 123, "Rider "+ch.Code); !errors.Is(err, ErrZwiftProof) {
		t.Fatal("other account reused proof", err)
	}
	if err := s.ConfirmZwiftLink(first, ch, 124, "Rider "+ch.Code); !errors.Is(err, ErrZwiftProof) {
		t.Fatal("other rider reused proof", err)
	}
	if err := s.ConfirmZwiftLink(first, ch, 123, "Rider"); !errors.Is(err, ErrZwiftProof) {
		t.Fatal("unproven link", err)
	}
	if err := s.ConfirmZwiftLink(first, ch, 123, "Rider "+ch.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginZwiftLink(second, 123); !errors.Is(err, ErrZwiftClaimed) {
		t.Fatal("duplicate rider", err)
	}
	a, _, err := s.Authenticate(first)
	if err != nil || a.Zwift == nil || a.Zwift.AthleteID != 123 || a.Zwift.Name != "Rider" {
		t.Fatal(a, err)
	}
	if err := s.ConfirmZwiftLink(first, ch, 123, "Rider "+ch.Code); !errors.Is(err, ErrZwiftProof) {
		t.Fatal("proof replay", err)
	}
	if err := s.UnlinkZwift(first); err != nil {
		t.Fatal(err)
	}
	a, _, _ = s.Authenticate(first)
	if a.Zwift != nil {
		t.Fatal("link retained")
	}
	if _, err := s.BeginZwiftLink(second, 123); err != nil {
		t.Fatal("ownership not released", err)
	}
}

func TestZwiftChallengeExpiryReplacementAndRate(t *testing.T) {
	s, m, now := fixture(t)
	token, _ := login(t, s, m, "first@example.test", true)
	ch, err := s.BeginZwiftLink(token, 123)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.BeginZwiftLink(token, 123)
	if err != nil || again.Code != ch.Code {
		t.Fatal("lost resumable challenge", err)
	}
	if _, err := s.BeginZwiftLink(token, 124); !errors.Is(err, ErrRate) {
		t.Fatal("challenge rate bypass", err)
	}
	*now = now.Add(time.Minute)
	next, err := s.BeginZwiftLink(token, 124)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmZwiftLink(token, ch, 123, "Rider "+ch.Code); !errors.Is(err, ErrZwiftProof) {
		t.Fatal("replaced challenge accepted", err)
	}
	*now = now.Add(16 * time.Minute)
	if err := s.ConfirmZwiftLink(token, next, 124, "Rider "+next.Code); !errors.Is(err, ErrZwiftProof) {
		t.Fatal("expired proof accepted", err)
	}
	if err := s.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ZwiftChallenge(token); !errors.Is(err, ErrZwiftProof) {
		t.Fatal(err)
	}
}

func TestZwiftOwnershipSurvivesRestart(t *testing.T) {
	s, m, _ := fixture(t)
	token, _ := login(t, s, m, "owner@example.test", true)
	other, _ := login(t, s, m, "other@example.test", true)
	ch, err := s.BeginZwiftLink(token, 456)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmZwiftLink(token, ch, 456, "Rider "+ch.Code); err != nil {
		t.Fatal(err)
	}
	cfg := s.cfg
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	a, _, err := reopened.Authenticate(token)
	if err != nil || a.Zwift == nil || a.Zwift.AthleteID != 456 {
		t.Fatal("lost persisted link", err)
	}
	if _, err := reopened.BeginZwiftLink(other, 456); !errors.Is(err, ErrZwiftClaimed) {
		t.Fatal("lost uniqueness across restart", err)
	}
}
