package bwitter

import (
	"errors"
	"fmt"
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
	coordAddress string
	numClients   int

	peersList []*rpc.Client
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

var coordClient *rpc.Client
var peerFailed chan *rpc.Client

func (m *Miner) Start(coordAddress string, numClients int) error {

	err := rpc.Register(m)
	CheckErr(err, "Failed to register Miner")

	coordClient, err = rpc.Dial("tcp", coordAddress)
	CheckErr(err, "Failed to establish connection between Miner and Coord")

	initialJoin(m.peersList, numClients)

	return errors.New("not implemented")
}

func initialJoin(peersList []*rpc.Client, numClients int) {
	newRequestedPeers := callCoordGetPeers(numClients)
	peersList = addNewMinerToPeersList(newRequestedPeers, peersList)

	coordClient.Call("Coord.NotifyJoin", nil, nil)

	go maintainPeersList(peersList, numClients)
}

func maintainPeersList(peersList []*rpc.Client, numClients int) {

	peerFailed = make(chan *rpc.Client)

	for {
		select {
		case failedClient := <-peerFailed:
			peersList = removeFailedMiner(failedClient, peersList)
			newRequestedPeers := callCoordGetPeers(1)
			peersList = addNewMinerToPeersList(newRequestedPeers, peersList)
		default:
			if len(peersList) < numClients {
				newRequestedPeers := callCoordGetPeers(numClients - len(peersList))
				peersList = addNewMinerToPeersList(newRequestedPeers, peersList)
			}
		}
	}
}

func removeFailedMiner(failedClient *rpc.Client, peersList []*rpc.Client) []*rpc.Client {

	var newList []*rpc.Client

	for _, miner := range peersList {
		if failedClient != miner {
			newList = append(newList, miner)
		}
	}

	return newList
}

func callCoordGetPeers(numRequested int) []string {

	var CoordGetPeerResponse CoordGetPeerResponse

	err := coordClient.Call("Coord.GetPeers", numRequested, &CoordGetPeerResponse)
	if err != nil {
		fmt.Println("unable to complete call to Coord.GetPeers")
	}

	return CoordGetPeerResponse.PeerList
}

func addNewMinerToPeersList(newRequestedPeers []string, peersList []*rpc.Client) []*rpc.Client {

	//TODO: check for dups

	var toAppend []*rpc.Client

	for _, peer := range newRequestedPeers {
		peerConnection, err := rpc.Dial("tcp", peer)
		if err != nil {
			continue
		}

		toAppend = append(toAppend, peerConnection)
	}

	return append(peersList, toAppend...)
}

func (m *Miner) Post(postArgs *PostArgs, response *PostResponse) error {

	return errors.New("not implemented")
}

// try a bunch of nonces on current block of transactions, as transactions change
func mineBlock() {

}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
