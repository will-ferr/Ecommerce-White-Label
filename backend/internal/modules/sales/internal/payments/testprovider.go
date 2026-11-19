package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SignatureHeader carries the signature of a test provider notification.
const SignatureHeader = "Test-Signature"

// signatureTolerance limits how old a signed notification may be, which
// stops a captured notification from being replayed later.
const signatureTolerance = 5 * time.Minute

// TestProvider stands in for a gateway so the whole purchase can be exercised.
// It is labeled as a test everywhere, refused by the configuration in
// production, and confirms payments through signed notifications delivered to
// the same webhook flow a real gateway uses.
type TestProvider struct {
	key []byte
	now func() time.Time
}

// NewTestProvider creates a TestProvider that signs notifications with key.
func NewTestProvider(key []byte) *TestProvider {
	return &TestProvider{key: key, now: time.Now}
}

// Name implements Provider.
func (*TestProvider) Name() string { return "test" }

// Label implements Provider.
func (*TestProvider) Label() string { return "Pagamento de teste" }

// CreateCharge implements Provider.
func (*TestProvider) CreateCharge(context.Context, Charge) (string, error) {
	return "test_" + uuid.NewString(), nil
}

// Refund implements Provider. The test provider returns money immediately.
func (*TestProvider) Refund(context.Context, string, int64, string) error { return nil }

type testNotification struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
}

func (p *TestProvider) sign(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Simulate produces the signed notification a gateway would send after the
// buyer approves or refuses the payment.
func (p *TestProvider) Simulate(reference, status string, amount int64) (http.Header, []byte, error) {
	body, err := json.Marshal(testNotification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: status, Amount: amount})
	if err != nil {
		return nil, nil, err
	}
	timestamp := strconv.FormatInt(p.now().Unix(), 10)
	header := http.Header{}
	header.Set(SignatureHeader, "t="+timestamp+",v1="+p.sign(timestamp, body))
	return header, body, nil
}

// ParseNotification implements Provider.
func (p *TestProvider) ParseNotification(header http.Header, body []byte) (Notification, error) {
	var timestamp, signature string
	for part := range strings.SplitSeq(header.Get(SignatureHeader), ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch key {
		case "t":
			timestamp = value
		case "v1":
			signature = value
		}
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || signature == "" {
		return Notification{}, fmt.Errorf("%w: missing signature", ErrInvalidNotification)
	}
	if age := p.now().Sub(time.Unix(seconds, 0)); age > signatureTolerance || age < -signatureTolerance {
		return Notification{}, fmt.Errorf("%w: signature expired", ErrInvalidNotification)
	}
	if !hmac.Equal([]byte(signature), []byte(p.sign(timestamp, body))) {
		return Notification{}, fmt.Errorf("%w: signature mismatch", ErrInvalidNotification)
	}

	var raw testNotification
	if err := json.Unmarshal(body, &raw); err != nil || raw.ID == "" || raw.Reference == "" {
		return Notification{}, fmt.Errorf("%w: malformed body", ErrInvalidNotification)
	}
	switch raw.Status {
	case StatusCaptured, StatusFailed, StatusRefunded:
	default:
		return Notification{}, fmt.Errorf("%w: unknown status", ErrInvalidNotification)
	}
	return Notification{EventID: raw.ID, Reference: raw.Reference, Status: raw.Status, Amount: &raw.Amount}, nil
}
