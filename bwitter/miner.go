package bwitter

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/rpc"
	"os"
	"time"
)

type PostArgs struct {
	MessageContents string
	Timestamp       time.Time
	PublicKey       rsa.PublicKey
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
	TargetBits      int
	Target          *big.Int
	MiningBlock     MiningBlock
	PeersList       []*rpc.Client

	CoordClient *rpc.Client
	PeerFailed  chan *rpc.Client

	TransactionsList []Transaction
}

type MiningBlock struct {
	MinerID      string
	Transactions []Transaction
	Nonce        int64
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
	CheckErr(err, "Failed to register Miner: %v\n", err)

	// TODO READ TARGET BITS FROM CONFIG, SETS DIFFICULTY
	// inspired by gochain
	m.TargetBits = 10
	m.Target = big.NewInt(1)
	m.Target.Lsh(m.Target, uint(256-m.TargetBits))

	m.CoordAddress = coordAddress
	m.MinerListenAddr = minerListenAddr
	m.NumClients = numClients

	minerListener, err := net.Listen("tcp", m.MinerListenAddr)
	if err != nil {
		return err
	}

	go rpc.Accept(minerListener)

	m.CoordClient, err = rpc.Dial("tcp", m.CoordAddress)
	CheckErr(err, "Failed to establish connection between Miner and Coord: %v\n", err)

	m.initialJoin(m.NumClients)

	for {

	}

	return nil
}

func (m *Miner) initialJoin(numClients int) {
	newRequestedPeers := m.callCoordGetPeers(numClients)
	m.addNewMinerToPeersList(newRequestedPeers)

	m.CoordClient.Call("Coord.NotifyJoin", nil, nil)

	go m.maintainPeersList(numClients)
}

func (m *Miner) maintainPeersList(numClients int) {

	m.PeerFailed = make(chan *rpc.Client)

	for {
		select {
		case failedClient := <-m.PeerFailed:
			m.removeFailedMiner(failedClient)
			newRequestedPeers := m.callCoordGetPeers(1)
			m.addNewMinerToPeersList(newRequestedPeers)
		default:
			if len(m.PeersList) < numClients {
				newRequestedPeers := m.callCoordGetPeers(numClients - len(m.PeersList))
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
	msgBytes := []byte(postArgs.MessageContents)
	// hash
	msgHash := sha256.New()
	_, err := msgHash.Write(msgBytes)
	if err != nil {
		panic(err)
	}
	msgHashSum := msgHash.Sum(nil)

	// Attempt decryption
	err = rsa.VerifyPSS(&postArgs.PublicKey, crypto.SHA256, msgHashSum, postArgs.SignedOperation, nil)
	CheckErr(err, "Failed to verify signature: %v\n", err)

	// if decryption successful, create Transaction and add to list
	transaction := Transaction{Timestamp: postArgs.Timestamp, Tweet: postArgs.MessageContents}
	m.TransactionsList = append(m.TransactionsList, transaction)

	// propagate op [JOSH]

	return nil
}

// try a bunch of nonces on current block of transactions, as transactions change
func (m *Miner) mineBlock() {
	var hashInteger big.Int
	var hash [32]byte

	nonce := int64(0)
	for nonce < math.MaxInt64 {
		m.MiningBlock.Nonce = nonce
		// add lock here
		blockBytes := m.convertBlockToBytes()
		// unlock here
		if blockBytes != nil {
			hash = sha256.Sum256(blockBytes)
			// Convert hash array to slice with [:]
			hashInteger.SetBytes(hash[:])
			// this will be true if the hash computed has the first m.TargetBits as 0
			if hashInteger.Cmp(m.Target) == -1 {
				break
			}
		}
		nonce++
	}
	// value is now in m.MiningBlock, maybe feed this to a channel that is waiting on it to broadcast to other nodes?
	return
}

func (m *Miner) convertBlockToBytes() []byte {
	var data bytes.Buffer
	enc := gob.NewEncoder(&data)
	err := enc.Encode(m.MiningBlock)
	if err != nil {
		return nil
	}
	return data.Bytes()
}

// validate block has two parts
// A) check proof of work hash actually corresponds to block
// B) check transactions make sense
func (m *Miner) validateBlock() {

}

func (m *Miner) validatePoW(block MiningBlock, givenHash big.Int) bool {
	var verifyHashInteger big.Int
	var hash [32]byte

	blockBytes := m.convertBlockToBytes()
	hash = sha256.Sum256(blockBytes)
	// Convert hash array to slice with [:]
	verifyHashInteger.SetBytes(hash[:])
	// Check if the hash given is the same as the hash generate from the block
	if verifyHashInteger.Cmp(&givenHash) == 0 {
		return true
	}
	return false
}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
