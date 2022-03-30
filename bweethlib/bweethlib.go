package bweethlib

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/gob"
	"log"
	"net"
	"net/rpc"
	"time"
)

type Bweeth struct {
	notifyCh   NotifyChannel
	privateKey *rsa.PrivateKey
	miner      *rpc.Client
}

// NotifyChannel is used for notifying the client about a mining result.
type NotifyChannel chan ResultStruct

type ResultStruct struct {
	txId      string // this should be a combination of public key and timestamp
	blockHash string
}

func NewBweeth() *Bweeth {
	return &Bweeth{}
}

// Start Starts the instance of Bweeth to use for connecting to the system with the given miner's IP:port.
// Private key should be the private key of some miner - this client will be spending that miner's bweeth balance
// The returned notify-channel channel must have capacity ChCapacity and must be used to deliver all post notifications
// ChCapacity determines the concurrency factor at the client: the client will never have more than ChCapacity number of operations outstanding (pending concurrently) at any one time.
// If there is an issue with connecting to a miner, this should return an appropriate err value, otherwise err should be set to nil.
func (b *Bweeth) Start(privateKey *rsa.PrivateKey, minerIPPort string, chCapacity int) (NotifyChannel, error) {
	b.privateKey = privateKey
	b.notifyCh = make(chan ResultStruct, chCapacity)

	raddr, err := net.ResolveTCPAddr("tcp", minerIPPort)
	if err != nil {
		return nil, err
	}

	// Generate unused port number
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}

	listenerAddress := listener.Addr().String()

	log.Println("addr", listenerAddress)

	laddr, err := net.ResolveTCPAddr("tcp", listenerAddress)
	if err != nil {
		return nil, err
	}
	listener.Close() // close the port we just grabbed
	conn, err := net.DialTCP("tcp", laddr, raddr)
	if err != nil {
		return nil, err
	}

	conn.SetLinger(0)

	b.miner = rpc.NewClient(conn)

	log.Println("Bweeth Started.")

	return b.notifyCh, nil
}

type PostArgs struct {
	MessageContents string
	Timestamp       time.Time
	PublicKey       rsa.PublicKey
	HashedMsg       []byte
	SignedOperation []byte
}
type PostResponse struct {
}

type MessageContent struct {
	Message   string
	Timestamp time.Time
}

// Post  non-blocking request from the client
// In case there is an underlying issue (for example, miner cannot be reached),
// this should return an appropriate err value, otherwise err should be set to nil. Note that this call is non-blocking.
// The returned value must be delivered asynchronously to the client via the notify-channel channel returned in the Start call.
// The string (txId) is used to identify this request and associate the returned value with this request.
func (b *Bweeth) Post(msg string) (string, error) {
	var network bytes.Buffer        // Stand-in for a network connection
	enc := gob.NewEncoder(&network) // Will write to network.
	// Encode (send) the value.
	now := time.Now()
	msgContent := MessageContent{msg, now}
	log.Println("msg", msgContent)
	err := enc.Encode(msgContent)
	if err != nil {
		log.Fatal("encode error:", err)
	}

	// HERE ARE YOUR BYTES!!!!
	msgBytes := network.Bytes()

	// https://www.sohamkamani.com/golang/rsa-encryption/#signing-and-verification
	msgHash := sha256.New()
	_, err = msgHash.Write(msgBytes)
	if err != nil {
		panic(err)
	}
	msgHashSum := msgHash.Sum(nil)

	// In order to generate the signature, we provide a random number generator,
	// our private key, the hashing algorithm that we used, and the hash sum
	// of our message
	signature, err := rsa.SignPSS(rand.Reader, b.privateKey, crypto.SHA256, msgHashSum, nil)
	if err != nil {
		panic(err)
	}

	var reply PostResponse
	b.miner.Call("Miner.Post", PostArgs{msg, now, b.privateKey.PublicKey, msgHashSum, signature}, &reply)

	return "TODO: txId", nil
}

// Stop Stops the Bweet instance and from delivering any results via the notify-channel.
// This call always succeeds.
func (b *Bweeth) Stop() {
	b.miner.Close()
}
