package common

import (
	"crypto/rand"
	"encoding/base64"
	"io"
)

type IDGenerator interface {
	NewID() (string, error)
}

type CryptoIDGenerator struct{}

func (CryptoIDGenerator) NewID() (string, error) {
	raw := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func ValidateID(id string) error {
	if len(id) != 32 {
		return NewError(CodeInvalidRequest, "invalid resource id", nil)
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(raw) != 24 {
		return NewError(CodeInvalidRequest, "invalid resource id", err)
	}
	return nil
}
