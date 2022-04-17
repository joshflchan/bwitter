package bweethlib

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"log"
	"net"
	"net/rpc"
	"strings"
	"time"

	"cs.ubc.ca/cpsc416/p2/bwitter/util"
)

type Bweeth struct {
	notifyCh        NotifyChannel
	PublicKeyString string
	privateKey      *rsa.PrivateKey
	miner           *rpc.Client
}

// NotifyChannel is used for notifying the client about a mining result.
type NotifyChannel chan ResultStruct

type ResultStruct struct {
	txId string // this should be a combination of public key and timestamp
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

	keyPem := string(pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: x509.MarshalPKCS1PublicKey(b.privateKey.Public().(*rsa.PublicKey)),
		},
	))
	keyLines := strings.Split(keyPem, "\n")
	keyWithNoDelimiters := keyLines[1 : len(keyLines)-2]
	keyString := strings.Join(keyWithNoDelimiters[:], "")
	b.PublicKeyString = keyString

	raddr, err := net.ResolveTCPAddr("tcp", minerIPPort)
	if err != nil {
		return nil, err
	}

	listenerAddress, err := util.GetAddressWithUnusedPort("")
	if err != nil {
		return nil, err
	}
	laddr, err := net.ResolveTCPAddr("tcp", listenerAddress)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTCP("tcp", laddr, raddr)
	if err != nil {
		return nil, err
	}
	conn.SetLinger(0)

	b.miner = rpc.NewClient(conn)

	log.Println("Bweeth Started.")

	return b.notifyCh, nil
}

// Post request from the client
// In case there is an underlying issue (for example, miner cannot be reached),
// this should return an appropriate err value, otherwise err should be set to nil. Note that this call is non-blocking.
// The returned value must be delivered asynchronously to the client via the notify-channel channel returned in the Start call.
// The string (txId) is used to identify this request and associate the returned value with this request.
func (b *Bweeth) Post(msg string) error {
	// Encode (send) the value.
	now := time.Now()
	msgContent := msg + now.String()
	log.Println("Client wants to send message:", msgContent)

	// https://www.sohamkamani.com/golang/rsa-encryption/#signing-and-verification
	msgHash := sha256.New()
	_, err := msgHash.Write([]byte(msgContent))
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

	var reply util.PostResponse
	postTimestamp := now.String()
	postErr := b.miner.Call("Miner.Post", util.PostArgs{
		MessageContents: msg,
		Timestamp:       postTimestamp,
		PublicKey:       &b.privateKey.PublicKey,
		PublicKeyString: b.PublicKeyString,
		SignedOperation: signature},
		&reply)
	if postErr != nil {
		log.Println("Bweethlib POST error:", postErr)
		return postErr
	}
	b.notifyCh <- ResultStruct{
		txId: b.PublicKeyString + postTimestamp,
	}
	return nil
}

// Stop Stops the Bweet instance and from delivering any results via the notify-channel.
// This call always succeeds.
func (b *Bweeth) Stop() {
	b.miner.Close()
}
