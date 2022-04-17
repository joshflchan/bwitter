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

type GetTweetsArgs struct {
}

type GetTweetsResponse struct {
	BlockTweetsChannel chan []string
	NotifyChannel      chan bool
}
