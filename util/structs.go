package util

import "crypto/rsa"

type PostArgs struct {
	MessageContents string
	Timestamp       string
	PublicKeyString string
	PublicKey       *rsa.PublicKey
	SignedOperation []byte
}

type PostResponse struct {
}
