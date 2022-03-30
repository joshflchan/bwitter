package bweethlib

import (
	"log"
	"net"
	"net/rpc"
)

type Bweeth struct {
	notifyCh   NotifyChannel
	privateKey string
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
func (b *Bweeth) Start(privateKey string, minerIPPort string, chCapacity int) (NotifyChannel, error) {
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

	return b.notifyCh, nil
}

// Post  non-blocking request from the client
// In case there is an underlying issue (for example, miner cannot be reached),
// this should return an appropriate err value, otherwise err should be set to nil. Note that this call is non-blocking.
// The returned value must be delivered asynchronously to the client via the notify-channel channel returned in the Start call.
// The string (txId) is used to identify this request and associate the returned value with this request.
func (b *Bweeth) Post(msg string) (string, error) {
	return "asd", nil
}

// Stop Stops the Bweet instance and from delivering any results via the notify-channel.
// This call always succeeds.
func (b *Bweeth) Stop() {
	b.miner.Close()
}
