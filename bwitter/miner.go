package bwitter

import (
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"time"
)

type PostArgs struct {
	MessageContents string
	Timestamp       time.Time
	PublicKey       string
	SignedOperation string
}

type PostResponse struct {
}

type CoordGetPeerResponse struct {
	PeerList []string
}

type Miner struct {
	coordAddress    string
	minerListenAddr string
	numClients      int

	peersList   []*rpc.Client
	coordClient *rpc.Client
	peerFailed  chan *rpc.Client
}

type MiningBlock struct {
	minerID      string
	transactions []Transaction
	nonce        string
	prevHash     string
}

type Transaction struct {
	timestamp time.Time
	tweet     string
}

func NewMiner() *Miner {
	return &Miner{}
}

func (m *Miner) Start(coordAddress string, minerListenAddr string, numClients int) error {

	err := rpc.Register(m)
	CheckErr(err, "Failed to register Miner")

	m.coordAddress = coordAddress
	m.minerListenAddr = minerListenAddr
	m.numClients = numClients

	minerListener, err := net.Listen("tcp", m.minerListenAddr)
	if err != nil {
		return err
	}

	go rpc.Accept(minerListener)

	m.coordClient, err = rpc.Dial("tcp", m.coordAddress)
	CheckErr(err, "Failed to establish connection between Miner and Coord")

	m.initialJoin(m.peersList, m.numClients)

	return nil
}

func (m *Miner) initialJoin(peersList []*rpc.Client, numClients int) {
	newRequestedPeers := m.callCoordGetPeers(numClients)
	m.addNewMinerToPeersList(newRequestedPeers)

	m.coordClient.Call("Coord.NotifyJoin", nil, nil)

	go m.maintainPeersList(peersList, numClients)
}

func (m *Miner) maintainPeersList(peersList []*rpc.Client, numClients int) {

	m.peerFailed = make(chan *rpc.Client)

	for {
		select {
		case failedClient := <-m.peerFailed:
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

	for _, miner := range m.peersList {
		if failedClient != miner {
			newList = append(newList, miner)
		}
	}

	m.peersList = newList
}

func (m *Miner) callCoordGetPeers(numRequested int) []string {

	var CoordGetPeerResponse CoordGetPeerResponse

	err := m.coordClient.Call("Coord.GetPeers", numRequested, &CoordGetPeerResponse)
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
	m.peersList = append(m.peersList, toAppend...)
}

func (m *Miner) Post(postArgs *PostArgs, response *PostResponse) error {

	return errors.New("not implemented")
}

// try a bunch of nonces on current block of transactions, as transactions change
func (m *Miner) mineBlock() {

}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
