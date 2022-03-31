package bwitter

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/gob"
	"log"
	"math"
	"math/big"
	"net"
	"net/rpc"
	"time"

	fchecker "cs.ubc.ca/cpsc416/p2/bwitter/fcheck"
	"cs.ubc.ca/cpsc416/p2/bwitter/util"
)

type PostArgs struct {
	MessageContents string
	Timestamp       time.Time
	PublicKey       rsa.PublicKey
	SignedOperation []byte
}

type PostResponse struct {
}
type MessageContent struct {
	Message   string
	Timestamp time.Time
}

type CoordGetPeerResponse struct {
	PeerList []string
}

type Miner struct {
	CoordAddress     string
	MinerListenAddr  string
	ExpectedNumPeers int
	TargetBits       int
	Target           *big.Int
	MiningBlock      MiningBlock
	PeersList        []*rpc.Client // doesn't need lock because modification of list only occurs in one goroutine;
	CoordClient      *rpc.Client
	PeerFailed       chan *rpc.Client

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

func (m *Miner) Start(coordAddress string, minerListenAddr string, expectedNumPeers int, genesisBlock MiningBlock) error {
	err := rpc.Register(m)
	if err != nil {
		log.Println("Failed to RPC register Miner")
		return err
	}

	// TODO READ TARGET BITS FROM CONFIG, SETS DIFFICULTY
	// inspired by gochain
	m.TargetBits = 10
	m.Target = big.NewInt(1)
	m.Target.Lsh(m.Target, uint(256-m.TargetBits))

	m.CoordAddress = coordAddress
	m.MinerListenAddr = minerListenAddr
	m.ExpectedNumPeers = expectedNumPeers

	minerListener, err := net.Listen("tcp", m.MinerListenAddr)
	if err != nil {
		return err
	}

	go rpc.Accept(minerListener)

	m.CoordClient, err = rpc.Dial("tcp", m.CoordAddress)
	if err != nil {
		log.Println("Failed to establish connection between Miner and Coord")
		return err
	}

	err = m.initialJoin(genesisBlock)
	if err != nil {
		log.Println("Failed Join Protocol")
	}

	for {

	}

	return nil
}

// TODO: what happens if gensis block already mined by a single node...
// node goes down... and new node joins with no peers... does it mine the genesis block again? should coord keep track somehow?
// or is it eventually handled once the new node reaches k peers and attempts to propagate a shorter chain? what happens after?
func (m *Miner) initialJoin(genesisBlock MiningBlock) error {
	// Get peers from Coord and add to peersList
	newRequestedPeers := m.callCoordGetPeers(m.ExpectedNumPeers)
	m.addNewMinerToPeersList(newRequestedPeers)

	// For genesis block -- Coord returns no peers
	if len(m.PeersList) == 0 {
		m.MiningBlock = genesisBlock
		m.mineBlock()
	} else {
		// TODO: Get entire blockchain from a peer

		// TODO: Perform validation on chain and store on disk
	}
	// Start fcheck to acknowledge heartbeats from Coord before notifying Coord of Join
	fCheckAddrForCoord, err := startFCheckListenOnly(m.MinerListenAddr)
	if err != nil {
		log.Println("Failed to start fcheck in listen only mode")
		return err
	}
	// Notify Coord of Join
	coordNotifyJoinArgs := CoordNotifyJoinArgs{
		IncomingMinerAddr: m.MinerListenAddr,
		MinerFcheckAddr:   fCheckAddrForCoord,
	}
	err = m.CoordClient.Call("Coord.NotifyJoin", coordNotifyJoinArgs, &CoordNotifyJoinResponse{})
	if err != nil {
		log.Println("Failed RPC call Coord.NotifyJoin")
		return err
	}

	// Maintain peersList
	go m.maintainPeersList()
	return nil
}

func (m *Miner) maintainPeersList() {
	m.PeerFailed = make(chan *rpc.Client) // initialize channel to detect failed peers

	for {
		select {
		case failedClient := <-m.PeerFailed:
			m.removeFailedMiner(failedClient)
			newRequestedPeers := m.callCoordGetPeers(1)
			m.addNewMinerToPeersList(newRequestedPeers)
		default: // continuously check for expected num peers to build robustness of network
			if len(m.PeersList) < m.ExpectedNumPeers {
				newRequestedPeers := m.callCoordGetPeers(m.ExpectedNumPeers - len(m.PeersList))
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
		log.Println("unable to complete call to Coord.GetPeers")
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
	var network bytes.Buffer        // Stand-in for a network connection
	enc := gob.NewEncoder(&network) // Will write to network.
	// Encode (send) the value.
	msgContent := MessageContent{postArgs.MessageContents, postArgs.Timestamp}
	err := enc.Encode(msgContent)
	if err != nil {
		log.Println("encode error:", err)
	}

	// HERE ARE YOUR BYTES!!!!
	msgBytes := network.Bytes()

	// hash
	msgHash := sha256.New()
	_, err = msgHash.Write(msgBytes)
	if err != nil {
		panic(err)
	}
	msgHashSum := msgHash.Sum(nil)

	// Attempt decryption
	err = rsa.VerifyPSS(&postArgs.PublicKey, crypto.SHA256, msgHashSum, postArgs.SignedOperation, nil)
	// CheckErr(err, "Failed to verify signature: %v\n", err)
	if err != nil {
		log.Println("Failed to verify signature for Post")
		return err
	}

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
	return verifyHashInteger.Cmp(&givenHash) == 0
}

func startFCheckListenOnly(nodeAddr string) (string, error) {
	// start fcheck in responding mode before connecting to coord
	fcheckInstance := fchecker.NewFcheck()

	ackLocalIPAckLocalPort, err := util.GetUnusedPort(nodeAddr)
	if err != nil {
		return "", err
	}

	log.Println("Using node listen address to ack for fcheck:", ackLocalIPAckLocalPort)
	_, fcheckErr := fcheckInstance.Start(
		fchecker.StartStruct{
			AckLocalIPAckLocalPort: ackLocalIPAckLocalPort,
		})
	if fcheckErr != nil {
		return "", fcheckErr
	}
	log.Println("Successfully started fcheck in listen only mode!")
	return ackLocalIPAckLocalPort, nil
}

// RPC call to peer node
func (m *Miner) getExistingChainFromPeer() {
	// TODO: Reading from disk
	// config file should have filepath for blockchain on disk storage

	// send entire chain in RPC repsonse
}

// COMMENTED OUT BEAUSE I THINK THIS SHOULD BE IN miner/main.go
// START SHOULD JUST RETURN ERRORS AND PROPAGATE UP
// func CheckErr(err error, errfmsg string, fargs ...interface{}) {
// 	if err != nil {
// 		log.Printf(os.Stderr, errfmsg, fargs...)
// 		os.Exit(1)
// 	}
// }
