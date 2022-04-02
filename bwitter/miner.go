package bwitter

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"math/big"
	"net"
	"net/rpc"
	"os"

	fchecker "cs.ubc.ca/cpsc416/p2/bwitter/fcheck"
	"cs.ubc.ca/cpsc416/p2/bwitter/util"
	"github.com/jinzhu/copier"
)

type Miner struct {
	CoordAddress     string
	MinerListenAddr  string
	ExpectedNumPeers uint64
	TargetBits       int
	Target           *big.Int
	MiningBlock      MiningBlock
	PeersList        []*rpc.Client // doesn't need lock because modification of list only occurs in one goroutine;
	CoordClient      *rpc.Client
	PeerFailed       chan *rpc.Client

	TransactionsList []Transaction
}

type MiningBlock struct {
	SequenceNum  int
	MinerID      string
	Transactions []Transaction
	Nonce        int64
	PrevHash     string
	CurrentHash  string
}

type Transaction struct {
	Timestamp string
	Tweet     string
}

func NewMiner() *Miner {
	return &Miner{}
}

func (m *Miner) Start(coordAddress string, minerListenAddr string, expectedNumPeers uint64, genesisBlock MiningBlock) error {
	err := rpc.Register(m)
	if err != nil {
		log.Println("Failed to RPC register Miner")
		return err
	}

	// TODO READ TARGET BITS FROM CONFIG, SETS DIFFICULTY
	// inspired by gochain
	m.TargetBits = 15
	m.Target = big.NewInt(1)
	m.Target.Lsh(m.Target, uint(256-m.TargetBits))

	m.CoordAddress = coordAddress
	m.MinerListenAddr = minerListenAddr
	m.ExpectedNumPeers = expectedNumPeers

	minerListener, err := net.Listen("tcp", m.MinerListenAddr)
	if err != nil {
		return err
	}

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
		rpc.Accept(minerListener)
	}
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
	} else {
		// TODO: Get entire blockchain from a peer

		// TODO: Perform validation on chain and store on disk

		// TODO: Set m.MiningBlock
	}
	// Start fcheck to acknowledge heartbeats from Coord before notifying Coord of Join
	fCheckAddrForCoord, err := startFCheckListenOnly(m.MinerListenAddr)
	if err != nil {
		log.Println("Failed to start fcheck in listen only mode")
		return err
	}
	// Notify Coord of Join
	joinArgs := CoordNotifyJoinArgs{
		IncomingMinerAddr: m.MinerListenAddr,
		MinerFcheckAddr:   fCheckAddrForCoord,
	}
	var joinResponse CoordNotifyJoinResponse
	log.Println("JOIN PROTOCOL: Requesting join")
	err = m.CoordClient.Call("Coord.NotifyJoin", joinArgs, &joinResponse)
	if err != nil {
		log.Println("Failed RPC call Coord.NotifyJoin")
		return err
	}
	log.Println("JOIN PROTOCOL: Join complete!")
	// Maintain peersList
	go m.maintainPeersList()
	// Start mining
	go m.mineBlock()
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
			lenOfExistingPeerList := uint64(len(m.PeersList))
			if lenOfExistingPeerList < m.ExpectedNumPeers {
				newRequestedPeers := m.callCoordGetPeers(m.ExpectedNumPeers - lenOfExistingPeerList)
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

func (m *Miner) callCoordGetPeers(numRequested uint64) []string {
	log.Println("JOIN PROTOCOL: Requesting peers")
	var getPeersResponse CoordGetPeersResponse
	getPeersArgs := CoordGetPeersArgs{
		IncomingMinerAddr: m.MinerListenAddr,
		ExpectedNumPeers:  numRequested,
	}
	err := m.CoordClient.Call("Coord.GetPeers", getPeersArgs, &getPeersResponse)
	if err != nil {
		log.Println("unable to complete call to Coord.GetPeers", err)
	}

	return getPeersResponse.NeighborAddrs
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

// RPC Call for client
func (m *Miner) Post(postArgs *util.PostArgs, response *util.PostResponse) error {
	log.Println("POST msg received:", postArgs.MessageContents)
	msgContent := postArgs.MessageContents + postArgs.Timestamp

	// HERE ARE YOUR BYTES!!!!
	// msgBytes := network.Bytes()

	// hash
	msgHash := sha256.New()
	_, err := msgHash.Write([]byte(msgContent))
	if err != nil {
		panic(err)
	}
	msgHashSum := msgHash.Sum(nil)
	// Attempt decryption
	err = rsa.VerifyPSS(&postArgs.PublicKey, crypto.SHA256, msgHashSum, postArgs.SignedOperation, nil)
	// CheckErr(err, "Failed to verify signature: %v\n", err)
	if err != nil {
		log.Println("Failed to verify signature for Post", err)
		return err
	}

	// if decryption successful, create Transaction and add to list
	transaction := Transaction{Timestamp: postArgs.Timestamp, Tweet: postArgs.MessageContents}
	// Not sure what this is for? Who uses that variable?
	m.TransactionsList = append(m.TransactionsList, transaction)

	// This is what we want so that it gets mined
	m.MiningBlock.Transactions = append(m.MiningBlock.Transactions, transaction)

	log.Println("tx:", transaction)
	// propagate op [JOSH]

	return nil
}

// try a bunch of nonces on current block of transactions, as transactions change
// Assumes m.MiningBlock is set externally
func (m *Miner) mineBlock() {
	for {
		log.Println("Mining block: ", m.MiningBlock)
		var block MiningBlock
		var hashInteger big.Int
		// is 32 necessary? maybe to chop off excess
		var hash [32]byte
		nonce := int64(0)
		for nonce < math.MaxInt64 {
			copier.Copy(&block, &m.MiningBlock)
			block.Nonce = nonce
			blockBytes := convertBlockToBytes(block)
			if blockBytes != nil {
				hash = sha256.Sum256(blockBytes)
				hashInteger.SetBytes(hash[:])
				// this will be true if the hash computed has the first m.TargetBits as 0
				if hashInteger.Cmp(m.Target) == -1 {
					block.CurrentHash = hex.EncodeToString(hash[:])
					break
				}

			}
			// unlock here?
			nonce++
		}
		// A) value is now in m.MiningBlock, maybe feed this to a channel that is waiting on it to broadcast to other nodes?
		// B) Probably call a function that appends the block to disk (on longest chain)
		// C) Since not using locks anymore, need function to compare hashed transactions to actual, and keep the difference

		oldSeqNum := block.SequenceNum // -> this could probably encapsulated in function mentioned in part C
		prevHash := block.CurrentHash  // -> this could probably encapsulated in function mentioned in part C

		m.MiningBlock.SequenceNum = oldSeqNum + 1 // -> this could probably encapsulated in function mentioned in part C
		m.MiningBlock.PrevHash = prevHash         // -> this could probably encapsulated in function mentioned in part C
	}
}

func convertBlockToBytes(block MiningBlock) []byte {
	var data bytes.Buffer
	enc := gob.NewEncoder(&data)
	err := enc.Encode(block)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
		return nil
	}
	return data.Bytes()
}

// validate block has two parts
// A) check proof of work hash actually corresponds to block
// B) check transactions make sense
func (m *Miner) validateBlock() {
	// call validatePow
	// call some function that checks transactions are valid using previous balances
}

func (m *Miner) validatePoW(block MiningBlock) bool {
	var computedHash [32]byte
	var computedHashInteger big.Int
	var givenHash [32]byte
	var givenHashInteger big.Int

	copy(givenHash[:], block.CurrentHash)
	block.CurrentHash = ""
	blockBytes := convertBlockToBytes(block)
	computedHash = sha256.Sum256(blockBytes)
	// Convert hash array to slice with [:]
	computedHashInteger.SetBytes(computedHash[:])
	givenHashInteger.SetBytes(givenHash[:])
	// Check if the hash given is the same as the hash generate from the block
	return computedHashInteger.Cmp(&givenHashInteger) == 0
}

func startFCheckListenOnly(nodeAddr string) (string, error) {
	// start fcheck in responding mode before connecting to coord
	fcheckInstance := fchecker.NewFcheck()

	ackLocalIPAckLocalPort, err := util.GetAddressWithUnusedPort(nodeAddr)
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
