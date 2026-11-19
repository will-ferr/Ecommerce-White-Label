package payments

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestTestProviderNotifications(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	provider := NewTestProvider([]byte("test-key-with-thirty-two-bytes!!"))
	provider.now = func() time.Time { return now }

	header, body, err := provider.Simulate("test_ref", StatusCaptured, 4990)
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.ParseNotification(header, body)
	if err != nil {
		t.Fatalf("ParseNotification() error = %v", err)
	}
	if got.Reference != "test_ref" || got.Status != StatusCaptured || got.Amount == nil || *got.Amount != 4990 || got.EventID == "" {
		t.Errorf("notification = %+v", got)
	}

	invalid := map[string]func() (http.Header, []byte){
		"changed body": func() (http.Header, []byte) {
			return header, []byte(`{"id":"evt","reference":"test_ref","status":"captured","amount":1}`)
		},
		"missing signature": func() (http.Header, []byte) { return http.Header{}, body },
		"other key": func() (http.Header, []byte) {
			forged, forgedBody, _ := NewTestProvider([]byte("another-key-with-thirty-two-byte")).Simulate("test_ref", StatusCaptured, 4990)
			return forged, forgedBody
		},
	}
	for name, build := range invalid {
		t.Run(name, func(t *testing.T) {
			h, b := build()
			if _, err := provider.ParseNotification(h, b); !errors.Is(err, ErrInvalidNotification) {
				t.Errorf("error = %v, want ErrInvalidNotification", err)
			}
		})
	}

	t.Run("old notification", func(t *testing.T) {
		provider.now = func() time.Time { return now.Add(6 * time.Minute) }
		defer func() { provider.now = func() time.Time { return now } }()
		if _, err := provider.ParseNotification(header, body); !errors.Is(err, ErrInvalidNotification) {
			t.Errorf("a replayed notification was accepted: %v", err)
		}
	})
}
