package ranking

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const signingProtocol = "keeper-ranking-center/v1"

type Identity struct {
	PublicKey  string
	PrivateKey string
}

func GenerateIdentity(random io.Reader) (Identity, error) {
	if random == nil {
		return Identity{}, fmt.Errorf("random source is required")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(random)
	if err != nil {
		return Identity{}, fmt.Errorf("generate ranking identity: %w", err)
	}
	return Identity{
		PublicKey:  base64.RawURLEncoding.EncodeToString(publicKey),
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey),
	}, nil
}

func (identity Identity) Sign(payload []byte) ([]byte, error) {
	privateKey, err := decodeKey(identity.PrivateKey, ed25519.PrivateKeySize)
	if err != nil {
		return nil, fmt.Errorf("decode ranking private key: %w", err)
	}
	return ed25519.Sign(ed25519.PrivateKey(privateKey), payload), nil
}

type SigningInput struct {
	Method         string
	Path           string
	Subject        string
	Sequence       *int64
	IdempotencyKey string
	Timestamp      time.Time
}

func CanonicalSigningPayload(input SigningInput, body []byte) ([]byte, error) {
	if input.Method == "" || input.Method != strings.ToUpper(input.Method) || strings.ContainsAny(input.Method, " \t\r\n") {
		return nil, fmt.Errorf("signing method is invalid")
	}
	if !strings.HasPrefix(input.Path, "/") || strings.ContainsAny(input.Path, "\r\n?#") {
		return nil, fmt.Errorf("signing path is invalid")
	}
	if input.Subject == "" || strings.ContainsAny(input.Subject, " \t\r\n") {
		return nil, fmt.Errorf("signing subject is invalid")
	}
	if input.Sequence != nil && *input.Sequence < 1 {
		return nil, fmt.Errorf("signing sequence must be positive")
	}
	if len(input.IdempotencyKey) > 64 || strings.ContainsAny(input.IdempotencyKey, " \t\r\n") {
		return nil, fmt.Errorf("signing idempotency key is invalid")
	}
	if input.Timestamp.IsZero() {
		return nil, fmt.Errorf("signing timestamp is required")
	}
	_, offset := input.Timestamp.Zone()
	if offset != 0 {
		return nil, fmt.Errorf("signing timestamp must use UTC")
	}

	sequence := "-"
	if input.Sequence != nil {
		sequence = strconv.FormatInt(*input.Sequence, 10)
	}
	idempotencyKey := input.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = "-"
	}
	bodyHash := sha256.Sum256(body)
	lines := []string{
		signingProtocol,
		input.Method,
		input.Path,
		input.Subject,
		sequence,
		idempotencyKey,
		input.Timestamp.Format(time.RFC3339Nano),
		base64.RawURLEncoding.EncodeToString(bodyHash[:]),
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func newIdempotencyKey(prefix string, random io.Reader) (string, error) {
	if prefix == "" || strings.ContainsAny(prefix, " \t\r\n") || random == nil {
		return "", fmt.Errorf("idempotency key source is invalid")
	}
	randomBytes := make([]byte, 24)
	if _, err := io.ReadFull(random, randomBytes); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(randomBytes), nil
}
