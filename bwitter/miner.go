package bwitter

import (
	"crypto"
	"crypto/rsa"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"time"
)

type PostArgs struct {
	MessageContents string
	Timestamp       time.Time
	PublicKey       rsa.PublicKey
	HashedMsg       []byte
	SignedOperation []byte
}

type PostResponse struct {
}

type CoordGetPeerResponse struct {
	PeerList []string
}

type Miner struct {
	CoordAddress    string
	MinerListenAddr string
	NumClients      int
	MiningBlock     MiningBlock
	PeersList       []*rpc.Client

	CoordClient *rpc.Client
	PeerFailed  chan *rpc.Client

	TransactionsList []Transaction
}

type MiningBlock struct {
	MinerID      string
	Transactions []Transaction
	Nonce        string
	PrevHash     string
}

type Transaction struct {
	Timestamp time.Time
	Tweet     string
}

func NewMiner() *Miner {
	return &Miner{}
}

func (m *Miner) Start(coordAddress string, minerListenAddr string, numClients int) error {

	err := rpc.Register(m)
	CheckErr(err, "Failed to register Miner")

	m.CoordAddress = coordAddress
	m.MinerListenAddr = minerListenAddr
	m.NumClients = numClients

	minerListener, err := net.Listen("tcp", m.MinerListenAddr)
	if err != nil {
		return err
	}

	go rpc.Accept(minerListener)

	m.CoordClient, err = rpc.Dial("tcp", m.CoordAddress)
	CheckErr(err, "Failed to establish connection between Miner and Coord")

	m.initialJoin(m.PeersList, m.NumClients)

	return nil
}

func (m *Miner) initialJoin(peersList []*rpc.Client, numClients int) {
	newRequestedPeers := m.callCoordGetPeers(numClients)
	m.addNewMinerToPeersList(newRequestedPeers)

	m.CoordClient.Call("Coord.NotifyJoin", nil, nil)

	go m.maintainPeersList(peersList, numClients)
}

func (m *Miner) maintainPeersList(peersList []*rpc.Client, numClients int) {

	m.PeerFailed = make(chan *rpc.Client)

	for {
		select {
		case failedClient := <-m.PeerFailed:
			m.removeFailedMiner(failedClient)
			newRequestedPeers := m.callCoordGetPeers(1)
			m.addNewMinerToPeersList(newRequestedPeers)
		default:
			if len(peersList) < numClients {
				newRequestedPeers := m.callCoordGetPeers(numClients - len(peersList))
				m.addNewMinerToPeersList(newRequestedPeers)
			}
		}
	}
}

func (m *Miner) removeFailedMiner(failedClient *rpc.Client) {

	var newList []*rpc.Client

	for _, miner := range m.PeersList {
		if failedClient != miner {
			newList = append(newList, miner)
		}
	}

	m.PeersList = newList
}

func (m *Miner) callCoordGetPeers(numRequested int) []string {

	var CoordGetPeerResponse CoordGetPeerResponse

	err := m.CoordClient.Call("Coord.GetPeers", numRequested, &CoordGetPeerResponse)
	if err != nil {
		fmt.Println("unable to complete call to Coord.GetPeers")
	}

	return CoordGetPeerResponse.PeerList
}

func (m *Miner) addNewMinerToPeersList(newRequestedPeers []string) {

	//TODO: check for dups

	var toAppend []*rpc.Client

	for _, peer := range newRequestedPeers {
		peerConnection, err := rpc.Dial("tcp", peer)
		if err != nil {
			continue
		}

		toAppend = append(toAppend, peerConnection)
	}
	m.PeersList = append(m.PeersList, toAppend...)
}

func (m *Miner) Post(postArgs *PostArgs, response *PostResponse) error {
	// Attempt decryption
	err := rsa.VerifyPSS(&postArgs.PublicKey, crypto.SHA256, postArgs.HashedMsg, postArgs.SignedOperation, nil)
	CheckErr(err, "Failed to verify signature: %v\n", err)

	// if decryption successful, create Transaction and add to list
	transaction := Transaction{Timestamp: postArgs.Timestamp, Tweet: postArgs.MessageContents}
	m.TransactionsList = append(m.TransactionsList, transaction)

	// propagate op [JOSH]

	return nil
}

// try a bunch of nonces on current block of transactions, as transactions change
func (m *Miner) mineBlock() {

}

// validate block has two parts
// A) check proof of work hash actually corresponds to block
// B) check transactions make sense
func (m *Miner) validateBlock() {

}

func (m *Miner) validatePoW() {

}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
