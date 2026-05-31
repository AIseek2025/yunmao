package feedstate

import (
	"errors"
	"testing"
)

func TestHappyPath(t *testing.T) {
	chain := []State{Created, Accepted, Queued, Dispatched, Acknowledged, Succeeded}
	cur := chain[0]
	for _, next := range chain[1:] {
		s, err := Transition(cur, next)
		if err != nil {
			t.Fatalf("%s -> %s: %v", cur, next, err)
		}
		cur = s
	}
	if !cur.IsTerminal() {
		t.Fatalf("%s should be terminal", cur)
	}
}

func TestRejectedFromCreated(t *testing.T) {
	if _, err := Transition(Created, Rejected); err != nil {
		t.Fatal(err)
	}
}

func TestFailedToCompensated(t *testing.T) {
	if _, err := Transition(Failed, Compensated); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidSkipping(t *testing.T) {
	if _, err := Transition(Created, Succeeded); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition error, got %v", err)
	}
}

func TestUnknownFrom(t *testing.T) {
	if _, err := Transition(State("ghost"), Accepted); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid")
	}
}

func TestCancelFromQueuedOrDispatched(t *testing.T) {
	if _, err := Transition(Queued, Rejected); err != nil {
		t.Fatalf("queued -> rejected (cancel) should be allowed: %v", err)
	}
	if _, err := Transition(Dispatched, Rejected); err != nil {
		t.Fatalf("dispatched -> rejected (cancel) should be allowed: %v", err)
	}
}
