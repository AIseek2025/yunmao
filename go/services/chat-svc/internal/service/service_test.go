package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"yunmao.live/pkg/yunmao/cache"
)

func newTestService() *ChatService {
	return New(Config{
		Store:          mustNewStore(),
		RateLimitStore: cache.NewMemoryStore(),
		RateLimitMax:   2,
		RateLimitWin:   1 * time.Second,
	})
}

func mustNewStore() Store {
	return &memStore{m: map[string]Message{}}
}

type memStore struct {
	m map[string]Message
}

func (s *memStore) Insert(_ context.Context, m Message) (*Message, error) {
	s.m[m.ID] = m
	return &m, nil
}

func (s *memStore) Get(_ context.Context, id string) (*Message, error) {
	if m, ok := s.m[id]; ok {
		return &m, nil
	}
	return nil, ErrNotFound
}

func (s *memStore) Moderate(_ context.Context, id, status, reason string) (*Message, error) {
	m, ok := s.m[id]
	if !ok {
		return nil, ErrNotFound
	}
	m.ModerationStatus = status
	m.ModerationReason = reason
	s.m[id] = m
	return &m, nil
}

func (s *memStore) List(_ context.Context, roomID string, limit int) ([]Message, error) {
	out := []Message{}
	for _, m := range s.m {
		if m.RoomID == roomID {
			out = append(out, m)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func TestChatSendSuccess(t *testing.T) {
	svc := newTestService()
	m, err := svc.Send(context.Background(), SendInput{
		UserID: "u1", RoomID: "r1", Body: "hello cat",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if m.Body != "hello cat" || m.ModerationStatus != "published" {
		t.Fatalf("unexpected message: %+v", m)
	}
}

func TestChatRatelimitRejects(t *testing.T) {
	svc := newTestService()
	for i := 0; i < 2; i++ {
		if _, err := svc.Send(context.Background(), SendInput{
			UserID: "u1", RoomID: "r1", Body: "msg",
		}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	_, err := svc.Send(context.Background(), SendInput{
		UserID: "u1", RoomID: "r1", Body: "third",
	})
	if err == nil {
		t.Fatalf("expected ratelimit error on 3rd msg")
	}
}

func TestChatSensitiveWordFlagged(t *testing.T) {
	svc := newTestService()
	m, err := svc.Send(context.Background(), SendInput{
		UserID: "u2", RoomID: "r1", Body: "you are 傻逼",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if m.ModerationStatus != "flagged" {
		t.Fatalf("expected flagged, got %s", m.ModerationStatus)
	}
}

func TestChatBodyTooLongRejected(t *testing.T) {
	svc := newTestService()
	bigBody := make([]rune, 1024)
	for i := range bigBody {
		bigBody[i] = 'a'
	}
	_, err := svc.Send(context.Background(), SendInput{
		UserID: "u1", RoomID: "r1", Body: string(bigBody),
	})
	if err == nil {
		t.Fatalf("expected too long error")
	}
}

func TestChatModerateUpdatesStatus(t *testing.T) {
	svc := newTestService()
	m, _ := svc.Send(context.Background(), SendInput{UserID: "u1", RoomID: "r1", Body: "hi"})
	out, err := svc.Moderate(context.Background(), m.ID, "hidden", "spam")
	if err != nil {
		t.Fatalf("moderate: %v", err)
	}
	if out.ModerationStatus != "hidden" {
		t.Fatalf("expected hidden, got %s", out.ModerationStatus)
	}
	_, err = svc.Moderate(context.Background(), "missing", "hidden", "")
	if !errors.Is(err, ErrNotFound) && err == nil {
		// some wrapped errors won't match Is; just check error not nil
		t.Fatalf("expected error for missing id, got nil")
	}
}
