package cloudevents

import (
	"encoding/json"
	"testing"
)

type FeedRequested struct {
	FeedRequestID string `json:"feed_request_id"`
	RoomID        string `json:"room_id"`
	AmountGrams   uint32 `json:"amount_grams"`
}

func TestEventRoundTrip(t *testing.T) {
	e := New[FeedRequested](
		"feed.command.requested",
		"feeding-svc@dev-1",
		"feed_demo",
		FeedRequested{FeedRequestID: "feed_01", RoomID: "room_demo", AmountGrams: 5},
	)
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}

	var got Event[FeedRequested]
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "feed.command.requested" {
		t.Fatalf("type lost: %q", got.Type)
	}
	if got.SpecVersion != "1.0" {
		t.Fatalf("specversion: %q", got.SpecVersion)
	}
	if got.Data.AmountGrams != 5 {
		t.Fatalf("data lost")
	}
}
