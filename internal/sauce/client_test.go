package sauce

import (
	"encoding/json"
	"testing"
)

const payload = `{"version":2,"athleteId":123,"self":true,"age":50,"courseId":1,"state":{"x":12345,"y":-4500,"z":1000,"courseId":1,"eventSubgroupId":7,"worldTime":320000,"sport":"cycling"}}`

func TestNormalize(t *testing.T) {
	var a Athlete
	if err := json.Unmarshal([]byte(payload), &a); err != nil {
		t.Fatal(err)
	}
	s, err := Normalize(a)
	if err != nil {
		t.Fatal(err)
	}
	if s.Position.X != 123.45 || s.Position.Y != -45 || s.Position.Z != 10 || s.Position.Event != 7 || s.Position.Realm != 1 || s.AthleteID != 123 {
		t.Fatalf("wrong units or identity: %+v", s)
	}
	for _, mutate := range []func(*Athlete){func(a *Athlete) { a.Self = false }, func(a *Athlete) { a.State.X = nil }, func(a *Athlete) { *a.Age = 3001 }, func(a *Athlete) { a.State.WorldTime = 0 }, func(a *Athlete) { a.Version = 1 }} {
		json.Unmarshal([]byte(payload), &a)
		mutate(&a)
		if _, err := Normalize(a); err == nil {
			t.Fatal("accepted incomplete, watched or stale data")
		}
	}
}
