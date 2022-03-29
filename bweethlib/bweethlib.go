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
	txId      string
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
