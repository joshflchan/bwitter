package bwitter

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"math/big"
	"math/rand"
	"net"
	"net/rpc"
	"os"
	"strings"
	"sync"
	"time"

	fchecker "cs.ubc.ca/cpsc416/p2/bwitter/fcheck"
	"cs.ubc.ca/cpsc416/p2/bwitter/util"
	"github.com/jinzhu/copier"
)

type Miner struct {
	MinerPublicKey     string
	CoordAddress       string
	MinerListenAddr    string
	ExpectedNumPeers   uint64
	RetryPeerThreshold uint8
	TargetBits         int
	miningLock         sync.Mutex
	Target             *big.Int
	MiningBlock        MiningBlock
	PeersList          map[string]*rpc.Client
	CoordClient        *rpc.Client
	PeerFailed         chan string
	ChainStorageFile   string
	broadcastChannel   chan MiningBlock

	BlocksSeen    map[string]bool // Hash of blocks seen
	MaxSeqNumSeen int

	PostsSeen  map[string]bool
	Ledger     map[string](map[string]int) // block hash: { publicKey: balance }
	CurrLedger map[string]int              // the most up-to-date ledger, used for keeping track of incoming operations

	ThresholdBlocks []MiningBlock
}

type Transaction struct {
	Address   string
	Timestamp string
	Tweet     string
}

type MiningBlock struct {
	SequenceNum    int
	MinerPublicKey string
	Transactions   []Transaction
	Nonce          int64
	PrevHash       string
	CurrentHash    string
	Timestamp      time.Time
}

type AddToPeersArgs struct {
	PeerAddress string
}

type AddToPeersResponse struct {
}

type PropagateArgs struct {
	Block MiningBlock
}

type PropagateResponse struct {
}

type GetExistingChainArgs struct {
	FileListenAddr string
}

type GetExistingChainResp struct {
}

const OUTPUT_DIR = "out/"

var (
	ErrInvalidChain      = errors.New("chain from peer was invalid")
	ErrStartFileServer   = errors.New("failed to start file transfer server")
	ErrInsufficientFunds = errors.New("miner does not have sufficient funds for this operation")
	infoLog              *log.Logger
)

func NewMiner() *Miner {
	return &Miner{}
}

func (m *Miner) Start(publicKey string, coordAddress string, minerListenAddr string, expectedNumPeers uint64, chainStorageFile string, genesisBlock MiningBlock, retryPeerThreshold uint8) error {
	infoLog = log.New(os.Stdout, fmt.Sprintf("MINER %v - ", minerListenAddr), log.Ldate|log.Lmicroseconds)

	err := rpc.Register(m)
	if err != nil {
		infoLog.Println("Failed to RPC register Miner")
		return err
	}

	// inspired by gochain
	m.TargetBits = 20
	m.Target = big.NewInt(1)
	m.Target.Lsh(m.Target, uint(256-m.TargetBits))

	m.MinerPublicKey = publicKey
	m.CoordAddress = coordAddress
	m.MinerListenAddr = minerListenAddr
	m.ExpectedNumPeers = expectedNumPeers
	m.PeersList = make(map[string]*rpc.Client)
	m.ChainStorageFile = chainStorageFile
	m.RetryPeerThreshold = retryPeerThreshold
	m.broadcastChannel = make(chan MiningBlock, 500)
	m.BlocksSeen = make(map[string]bool)
	m.PostsSeen = make(map[string]bool)
	m.Ledger = make(map[string]map[string]int)

	_, port, err := net.SplitHostPort(m.MinerListenAddr)
	if err != nil {
		return err
	}
	minerListener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return err
	}

	m.CoordClient, err = rpc.Dial("tcp", m.CoordAddress)
	if err != nil {
		infoLog.Println("Failed to establish connection between Miner and Coord")
		return err
	}

	err = m.initialJoin(genesisBlock)
	if err != nil {
		infoLog.Println("Failed Join Protocol")
		return err
	}

	for {
		rpc.Accept(minerListener)
	}
}

func (m *Miner) initialJoin(genesisBlock MiningBlock) error {
	infoLog.Println("Begin Join Protocol")
	// Get peers from Coord and add to peersList
	newRequestedPeers := m.callCoordGetPeers(m.ExpectedNumPeers)
	m.addNewMinerToPeersList(newRequestedPeers)
	// Maintain peersList
	go m.maintainPeersList()
	_, port, err := net.SplitHostPort(m.MinerListenAddr)
	if err != nil {
		return err
	}
	fileListenAddr, err := util.GetAddressWithUnusedPort(port)
	if err != nil {
		infoLog.Println(err)
		return err
	}
	doneTransfer := make(chan string, 1)
	errTransfer := make(chan error, 1)
	prepTransfer := make(chan bool, 1)

	// Start fcheck to acknowledge heartbeats from Coord and notify Coord of Join before validating chain,
	// so we can fill up channel with blocks we miss

	// TODO: REPLACE THIS WITH ADD YOURSELF AS A PEER TO THE NODE THAT GIVE YOU CHAIN.TXT
	fCheckAddrForCoord, err := startFCheckListenOnly(m.MinerListenAddr)
	if err != nil {
		infoLog.Println("Failed to start fcheck in listen only mode")
		return err
	}
	// Notify Coord of Join
	joinArgs := CoordNotifyJoinArgs{
		IncomingMinerAddr: m.MinerListenAddr,
		MinerFcheckAddr:   fCheckAddrForCoord,
	}
	var joinResponse CoordNotifyJoinResponse
	infoLog.Println("JOIN PROTOCOL: Requesting join")
	err = m.CoordClient.Call("Coord.NotifyJoin", joinArgs, &joinResponse)
	if err != nil {
		infoLog.Println("Failed RPC call Coord.NotifyJoin")
		return err
	}
	go m.startFileTransferServer(fileListenAddr, doneTransfer, errTransfer, prepTransfer)

ContinueJoinProtocol:
	for { // Try all peers in peer list
		numPeers := len(m.PeersList)
		if numPeers > 0 {
			randomIndex := rand.Intn(numPeers) // pick a random peer
			// get keys
			peerAddresses := make([]string, 0, numPeers)
			for k := range m.PeersList {
				peerAddresses = append(peerAddresses, k)
			}
			peerMiner := peerAddresses[randomIndex]
			for i := uint8(0); i < m.RetryPeerThreshold; i++ {
				getChainError := m.callGetExistingChain(peerMiner, fileListenAddr, doneTransfer, errTransfer, prepTransfer)
				if getChainError != nil {
					infoLog.Println("Error from callGetExistingChain", getChainError)
					if errors.Is(getChainError, ErrInvalidChain) {
						infoLog.Println("Removing peer from PeerList since given chain is invalid")
						break
					} else if errors.Is(getChainError, ErrStartFileServer) {
						return getChainError
					}
					infoLog.Printf("Attempt %v to get existing chain from peer (%v) failed... Trying again\n", i+1, peerMiner)
				} else {
					break ContinueJoinProtocol
				}
			}
			m.PeerFailed <- peerMiner
		} else {
			infoLog.Println("No peers available... using genesis block")
			m.MiningBlock = genesisBlock
			if _, err := os.Stat(OUTPUT_DIR + m.ChainStorageFile); !os.IsNotExist(err) {
				infoLog.Println("REJEOIN PROTOCOL: remove existing chain storage file because no peers")
				os.Remove(OUTPUT_DIR + m.ChainStorageFile)
			}
			break
		}
	}
	// Now that we have validated chain, start going through queue
	go m.validatePropagatedBlock()

	infoLog.Println("JOIN PROTOCOL: Join complete!")
	// Start mining
	go m.mineBlock()
	return nil
}

func (m *Miner) maintainPeersList() {
	m.PeerFailed = make(chan string) // initialize channel to detect failed peers
	for {
		select {
		case failedClient := <-m.PeerFailed:
			infoLog.Println("Detected failed peer... removing")
			m.removeFailedMiner(failedClient)
			newRequestedPeers := m.callCoordGetPeers(1)
			m.addNewMinerToPeersList(newRequestedPeers)
		default: // continuously check for expected num peers to build robustness of network
			lenOfExistingPeerList := uint64(len(m.PeersList))
			if lenOfExistingPeerList < m.ExpectedNumPeers {
				// infoLog.Println("not enough peers, requesting more")
				newRequestedPeers := m.callCoordGetPeers(m.ExpectedNumPeers - lenOfExistingPeerList)
				m.addNewMinerToPeersList(newRequestedPeers)
			}
			time.Sleep(time.Second) // wait a bit
		}
	}
}

func (m *Miner) removeFailedMiner(failedPeer string) {
	delete(m.PeersList, failedPeer)
}

func (m *Miner) callCoordGetPeers(numRequested uint64) []string {
	// infoLog.Println("JOIN PROTOCOL: Requesting peers")
	var getPeersResponse CoordGetPeersResponse
	getPeersArgs := CoordGetPeersArgs{
		IncomingMinerAddr: m.MinerListenAddr,
		ExpectedNumPeers:  numRequested,
	}
	err := m.CoordClient.Call("Coord.GetPeers", getPeersArgs, &getPeersResponse)
	if err != nil {
		infoLog.Println("unable to complete call to Coord.GetPeers", err)
		if errors.Is(err, rpc.ErrShutdown) { // exit if coord is down
			infoLog.Println(err)
			os.Exit(1)
		}
	}
	// infoLog.Println("getPeers response: ", getPeersResponse)
	return getPeersResponse.NeighborAddrs
}

func (m *Miner) addNewMinerToPeersList(newRequestedPeers []string) {
	for _, peer := range newRequestedPeers {
		if _, ok := m.PeersList[peer]; ok {
			continue
		} else {
			infoLog.Println("Adding new miner to peer list:", peer)
			peerConnection, err := rpc.Dial("tcp", peer)
			if err != nil {
				infoLog.Println("Unable to dial to peer: ", peer)
				continue
			}
			m.PeersList[peer] = peerConnection
			// Create two-way relationship
			var response AddToPeersResponse
			err = peerConnection.Call("Miner.AddToPeersList", &AddToPeersArgs{m.MinerListenAddr}, &response)
			if err != nil {
				infoLog.Println("Error from RPC Miner.AddToPeersList: ", err)
				continue
			}
			m.PeersList[peer] = peerConnection
		}
	}
}

func (m *Miner) AddToPeersList(addToPeersArgs *AddToPeersArgs, response *AddToPeersResponse) error {
	infoLog.Println("ADDING TO PEERS LIST: ", addToPeersArgs)
	peer := addToPeersArgs.PeerAddress
	if _, ok := m.PeersList[peer]; !ok {
		peerConnection, err := rpc.Dial("tcp", peer)
		if err != nil {
			infoLog.Println("Found an error :", err)
			return err
		}
		m.PeersList[peer] = peerConnection
	}
	infoLog.Println("AddToPeersList m.PeersList: ", m.PeersList)
	return nil
}

// RPC Call for client
func (m *Miner) Post(postArgs *util.PostArgs, response *util.PostResponse) error {
	infoLog.Println("POST msg received for operation by:", postArgs.PublicKeyString)

	if _, ok := m.PostsSeen[postArgs.PublicKeyString+postArgs.Timestamp]; ok {
		log.Println("POST has already been seen and added to block")
		return nil
	} else {
		m.PostsSeen[postArgs.PublicKeyString+postArgs.Timestamp] = true
	}

	msgContent := postArgs.MessageContents + postArgs.Timestamp

	// hash
	msgHash := sha256.New()
	_, err := msgHash.Write([]byte(msgContent))
	if err != nil {
		panic(err)
	}
	msgHashSum := msgHash.Sum(nil)
	// Attempt decryption
	err = rsa.VerifyPSS(postArgs.PublicKey, crypto.SHA256, msgHashSum, postArgs.SignedOperation, nil)
	// CheckErr(err, "Failed to verify signature: %v\n", err)
	if err != nil {
		infoLog.Println("Failed to verify signature for Post", err)
		return err
	}

	transaction := Transaction{Address: postArgs.PublicKeyString, Timestamp: postArgs.Timestamp, Tweet: postArgs.MessageContents}

	// Validate sufficient funds
	if !m.validateSufficientFunds(transaction, m.CurrLedger) {
		infoLog.Println("INSUFFICIENT FUNDS: ", m.CurrLedger[transaction.Address])
		return ErrInsufficientFunds
	}

	// Decrement funds
	m.CurrLedger[transaction.Address]--
	// infoLog.Println("new currLedger balance?:", m.CurrLedger[transaction.Address])
	// infoLog.Println("main ledger balance: ", m.Ledger[m.MiningBlock.PrevHash][transaction.Address])
	response.TweethRemaining = m.CurrLedger[transaction.Address]

	// This is what we want so that it gets mined
	m.MiningBlock.Transactions = append(m.MiningBlock.Transactions, transaction)

	// infoLog.Println("tx:", transaction)
	// infoLog.Println("txs on the current block being mined:", m.MiningBlock.Transactions)

	var reply util.PostResponse
retryPeer:
	for peerAddress, peerConnection := range m.PeersList {
		infoLog.Printf("Propagating to peer %v: %v", peerAddress, postArgs)
		for i := uint8(0); i < m.RetryPeerThreshold; i++ {
			err := peerConnection.Call("Miner.Post", postArgs, &reply)
			if err != nil {
				infoLog.Println("Error from RPC Miner.Post", err)
			} else {
				continue retryPeer
			}
		}
		m.PeerFailed <- peerAddress
	}

	return nil
}

func (m *Miner) GetTweets(getTweetsArgs *util.GetTweetsArgs, response *util.GetTweetsResponse) error {
	infoLog.Println("Received request for tweets!!")
	response.BlockStack = m.createBlockStack()

	return nil
}

func (m *Miner) createBlockStack() [][]string {
	prevHash := m.MiningBlock.PrevHash
	var blockStack [][]string
	for prevHash != "" {
		block, err := m.getBlock(prevHash)
		if err != nil {
			infoLog.Println("Failed to get block for hash: ", prevHash)
			infoLog.Println(err)
		}

		var blockTweets []string
		for _, tx := range block.Transactions {
			blockTweets = append(blockTweets, tx.Tweet)
		}

		infoLog.Println("blockStack BEFORE push: ", blockStack)
		blockStack = Push(blockStack, blockTweets)
		infoLog.Println("blockStack AFTER push: ", blockStack)

		prevHash = block.PrevHash
	}

	infoLog.Println(blockStack)
	return blockStack
}

func (m *Miner) getBlock(hashToFind string) (*MiningBlock, error) {
	src, err := os.Open(OUTPUT_DIR + m.ChainStorageFile)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	scanner := bufio.NewScanner(src)
	// Scan() reads next line and returns false when reached end or error
	var block *MiningBlock
	for scanner.Scan() {
		blockLineAsBytes := scanner.Bytes()
		// process the line
		block = new(MiningBlock)
		err = m.unmarshalBlock(blockLineAsBytes, block)
		if err != nil {
			return nil, err
		}

		if block.CurrentHash == hashToFind {
			infoLog.Println("find me", block)
			return block, nil
		}
	}

	return nil, nil
}

// try a bunch of nonces on current block of transactions, as transactions change
// Assumes m.MiningBlock is set externally
func (m *Miner) mineBlock() {
	for {
		var minedBlock MiningBlock
		var hashInteger big.Int
		// is 32 necessary? maybe to chop off excess
		var hash [32]byte
		nonce := int64(0)
		timestamp := time.Now()
		for nonce < math.MaxInt64 {
			m.miningLock.Lock()
			var block MiningBlock
			copier.CopyWithOption(&block, &m.MiningBlock, copier.Option{IgnoreEmpty: false, DeepCopy: true})
			block.Timestamp = timestamp
			block.MinerPublicKey = m.MinerPublicKey
			block.Nonce = nonce
			blockBytes := convertBlockToBytes(block)
			if blockBytes != nil {
				hash = sha256.Sum256(blockBytes)
				hashInteger.SetBytes(hash[:])
				// this will be true if the hash computed has the first m.TargetBits as 0
				if hashInteger.Cmp(m.Target) == -1 {
					block.CurrentHash = hex.EncodeToString(hash[:])
					infoLog.Println("MINED BLOCK: ", block)
					minedBlock = block
					m.miningLock.Unlock()
					break
				}

			}
			nonce++

			// if we havent found a nonce, try again from the start
			// because we might have skipped over it as transactions were changing
			if nonce == math.MaxInt64 {
				nonce = 0
			}
			m.miningLock.Unlock()
		}
		m.BlocksSeen[minedBlock.CurrentHash] = true
		m.generateBlockLedger(minedBlock)
		m.createNewMiningBlock(minedBlock)
		m.writeNewBlockToStorage(minedBlock)

		var reply PropagateResponse
	retryPeer:
		for peerAddress, peerConnection := range m.PeersList {
			infoLog.Printf("Propogate block to peer %v", peerAddress)

			for i := uint8(0); i < m.RetryPeerThreshold; i++ {
				err := peerConnection.Call("Miner.PropagateBlock", PropagateArgs{Block: minedBlock}, &reply)
				if err != nil {
					infoLog.Println("Error from Miner.PropagateBlock", err)
				} else {
					continue retryPeer
				}
			}
			m.PeerFailed <- peerAddress
		}
	}
}

func (m *Miner) generateBlockLedger(block MiningBlock) {
	ledger := make(map[string]int)
	if block.SequenceNum != 0 {
		ledger = copyMap(m.Ledger[block.PrevHash])
		// we MUST have seen the prevHash before, otherwise we are not adding to the blockchain
		if ledger == nil {
			fmt.Println("VALIDATION DIDNT WORK")
			os.Exit(1)
		}
	}

	if val, ok := ledger[block.MinerPublicKey]; ok {
		infoLog.Println(val)
		// a miner gets 1 bweeth for mining a block + n bweeth for n transactions on the mined block
		ledger[block.MinerPublicKey] = 1 + len(block.Transactions) + ledger[block.MinerPublicKey]
	} else {
		ledger[block.MinerPublicKey] = 1 + len(block.Transactions)
	}

	// recompute ledger based on transactiosn in block
	for _, tx := range block.Transactions {
		ledger[tx.Address]--
		// bal, ok := ledger[tx.Address] // for debugging
		// infoLog.Println("the bal is", bal, ok)
	}

	m.Ledger[block.CurrentHash] = copyMap(ledger)

	if block.SequenceNum == m.MiningBlock.SequenceNum && block.PrevHash == m.MiningBlock.PrevHash {
		m.CurrLedger = copyMap(ledger)
		infoLog.Println("updated current ledger")
	} else {
		infoLog.Println("old block, do not update current ledger")
	}
}

func copyMap(originalMap map[string]int) map[string]int {
	newMap := make(map[string]int)
	for key, value := range originalMap {
		newMap[key] = value
	}
	return newMap
}

func (m *Miner) writeNewBlockToStorage(minedBlock MiningBlock) {
	if minedBlock.SequenceNum > m.MaxSeqNumSeen {
		m.MaxSeqNumSeen = minedBlock.SequenceNum
	}
	if _, err := os.Stat(OUTPUT_DIR); os.IsNotExist(err) {
		if err := os.Mkdir(OUTPUT_DIR, os.ModePerm); err != nil {
			infoLog.Printf("Unable to create dir ./%v: %v\n", OUTPUT_DIR, err)
			return
		}
	}
	marshalledBlock, err := json.Marshal(minedBlock)
	if err != nil {
		infoLog.Println(err)
	}

	chainStoragePath := OUTPUT_DIR + m.ChainStorageFile

	stringToWrite := string(marshalledBlock)
	infoLog.Println("Writing block to storage: ", stringToWrite)
	if _, err := os.Stat(chainStoragePath); !os.IsNotExist(err) {
		stringToWrite = "\n" + stringToWrite
	}

	f, err := os.OpenFile(chainStoragePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		infoLog.Println(err)
	}
	defer f.Close()
	if _, err := f.WriteString(stringToWrite); err != nil {
		infoLog.Println(err)
		return
	}
	fmt.Println("WROTE NEW BLOCK TO STORAGE, nonce: ", minedBlock.Nonce)
}

func (m *Miner) PropagateBlock(propagateArgs *PropagateArgs, response *PropagateResponse) error {
	infoLog.Println("RECEIVED BLOCK FROM PEER: ", propagateArgs)
	m.broadcastChannel <- propagateArgs.Block
	return nil
}

func (m *Miner) validatePropagatedBlock() {
	for {
		propagatedBlock := <-m.broadcastChannel
		if propagatedBlock.SequenceNum < m.MaxSeqNumSeen-10 {
			log.Println("Not propagating this block - too old")
			continue
		}

		if propagatedBlock.Transactions == nil {
			propagatedBlock.Transactions = []Transaction{}
		}

		// Validate the block
		if !m.validateBlock(&propagatedBlock) {
			log.Println("Not propagating this block")
			continue
		}

		infoLog.Println("Block is successfully validated")
		m.writeNewBlockToStorage(propagatedBlock)

		// Propagate to peers
		var reply PropagateResponse
		propagateArgs := PropagateArgs{
			Block: propagatedBlock,
		}
	retryPeer:
		for peerAddress, peerConnection := range m.PeersList {
			infoLog.Printf("Propogate to peer %v", peerAddress)
			for i := uint8(0); i < m.RetryPeerThreshold; i++ {
				err := peerConnection.Call("Miner.PropagateBlock", propagateArgs, &reply)
				if err != nil {
					infoLog.Println("Error from RPC Miner.PropagateBlock", err)
				} else {
					continue retryPeer
				}
			}
			m.PeerFailed <- peerAddress
		}
	}
}

func (m *Miner) createNewMiningBlock(minedBlock MiningBlock) {
	oldSeqNum := minedBlock.SequenceNum
	prevHash := minedBlock.CurrentHash
	missingTransactions := []Transaction{}
	totalTransactions := len(m.MiningBlock.Transactions)
	minedTransactions := len(minedBlock.Transactions)
	if totalTransactions > minedTransactions {
		copy(missingTransactions, m.MiningBlock.Transactions[totalTransactions-(totalTransactions-minedTransactions):])
	}
	m.MiningBlock = MiningBlock{}

	// What is this stuff?????
	if oldSeqNum+1 > m.MaxSeqNumSeen {
		m.MiningBlock.SequenceNum = oldSeqNum + 1
	} else {
		m.MiningBlock.SequenceNum = m.MaxSeqNumSeen + 1
	}
	// shouldnt we update the max now?
	m.MiningBlock.PrevHash = prevHash
	// m.MiningBlock.MinerPublicKey = m.MinerPublicKey --> moved to mineBlock
	copy(m.MiningBlock.Transactions, missingTransactions)
}

func convertBlockToBytes(block MiningBlock) []byte {
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	err := enc.Encode(block)
	if err != nil {
		infoLog.Println(err)
		os.Exit(1)
		return nil
	}
	return data.Bytes()
}

// validate block has three parts
// 1) check if this hash has been seen already, short circuit if so
// 2) check proof of work hash actually corresponds to block
// 3) check transactions make sense
func (m *Miner) validateBlock(block *MiningBlock) bool {
	_, ok := m.BlocksSeen[block.CurrentHash]
	if ok {
		// seen this block already, ignore
		infoLog.Println("Block rejected - already seen")
		fmt.Println(block)
		return false
	}
	// if this isnt genesis block and it's pointing to a prevHash we have never seen, then this is a completely different chain
	if block.SequenceNum != 0 {
		_, ok := m.BlocksSeen[block.PrevHash]
		if !ok {
			infoLog.Println("Block rejected - is not building off any known hash")
			fmt.Println(block)
			return false
		}
	}

	// we can mark as seen even if this block would be found invalid in the future
	m.BlocksSeen[block.CurrentHash] = true

	// check if this is the current block we are mining, lock because we might need to update
	// avoids updating inflight after a broadcast has been triggered
	isCurrentBlock := block.SequenceNum == m.MiningBlock.SequenceNum &&
		block.PrevHash == m.MiningBlock.PrevHash
	if isCurrentBlock {
		m.miningLock.Lock()
		defer m.miningLock.Unlock()
	}

	// Validate PoW
	if !m.validatePoW(block) {
		infoLog.Println("Block rejected - invalid PoW")
		return false
	}

	// Valdiating transactions on the block
	ledgerCopy := copyMap(m.Ledger[block.PrevHash])

	recvdBlockTransactionsSet := make(map[Transaction]struct{})
	exists := struct{}{}
	for _, transaction := range block.Transactions {
		recvdBlockTransactionsSet[transaction] = exists
		if !m.validateSufficientFunds(transaction, ledgerCopy) {
			infoLog.Println("Block rejected - invalid transaction: ", transaction)
			return false
		}
		ledgerCopy[transaction.Address]--
	}
	m.generateBlockLedger(*block)

	if isCurrentBlock {
		// update because we passed the checks
		missingTransactions := []Transaction{}
		for _, transaction := range m.MiningBlock.Transactions {
			if _, ok := recvdBlockTransactionsSet[transaction]; !ok {
				missingTransactions = append(missingTransactions, transaction)
			}
		}
		m.MiningBlock = MiningBlock{}
		m.MiningBlock.SequenceNum = block.SequenceNum + 1
		m.MiningBlock.PrevHash = block.CurrentHash
		m.MiningBlock.MinerPublicKey = m.MinerPublicKey
		copy(m.MiningBlock.Transactions, missingTransactions)
	} else {
		infoLog.Println("Validated block is not current block, not updating our mining block")
	}

	// if valid, return true
	return true
}

func (m *Miner) validatePoW(block *MiningBlock) bool {
	var computedHash [32]byte
	var computedHashInteger big.Int
	var givenHash [32]byte
	var givenHashInteger big.Int

	givenHashDecoded, _ := hex.DecodeString(block.CurrentHash)
	copy(givenHash[:], givenHashDecoded)
	tempCurrentHash := block.CurrentHash
	block.CurrentHash = ""
	blockBytes := convertBlockToBytes(*block)
	computedHash = sha256.Sum256(blockBytes)
	fmt.Println(hex.EncodeToString(computedHash[:]))
	// Convert hash array to slice with [:]
	computedHashInteger.SetBytes(computedHash[:])
	givenHashInteger.SetBytes(givenHash[:])

	isValid := computedHashInteger.Cmp(&givenHashInteger) == 0
	isDifficultEnough := givenHashInteger.Cmp(m.Target) == -1
	block.CurrentHash = tempCurrentHash

	// Check if the hash given is the same as the hash generate from the block
	return isValid && isDifficultEnough
}

func (m *Miner) validateSufficientFunds(transaction Transaction, ledger map[string](int)) bool {
	infoLog.Println(ledger[transaction.Address])
	infoLog.Println(ledger[transaction.Address] >= 1)
	return ledger[transaction.Address] >= 1
}

func startFCheckListenOnly(nodeAddr string) (string, error) {
	// start fcheck in responding mode before connecting to coord
	fcheckInstance := fchecker.NewFcheck()

	ackLocalIPAckLocalPort, err := util.GetAddressWithUnusedPort(nodeAddr)
	if err != nil {
		return "", err
	}

	infoLog.Println("Using node listen address to ack for fcheck:", ackLocalIPAckLocalPort)
	_, fcheckErr := fcheckInstance.Start(
		fchecker.StartStruct{
			AckLocalIPAckLocalPort: ackLocalIPAckLocalPort,
		})
	if fcheckErr != nil {
		return "", fcheckErr
	}
	infoLog.Println("Successfully started fcheck in listen only mode!")
	return ackLocalIPAckLocalPort, nil
}

// RPC call to peer node
func (m *Miner) GetExistingChainFromPeer(args *GetExistingChainArgs, resp *GetExistingChainResp) error {
	// config file should have filepath for blockchain on disk storage
	infoLog.Println("JOIN Protocol: Attempt to send existing chain to miner")
	conn, err := net.Dial("tcp", args.FileListenAddr)
	if err != nil {
		infoLog.Println("There was an error making a connection")
		return err
	}
	//file to read
	file, err := os.Open(strings.TrimSpace(OUTPUT_DIR + m.ChainStorageFile))
	if err != nil {
		return err
	}
	defer file.Close()
	bytes, err := io.Copy(conn, file)
	if err != nil {
		return err
	}
	infoLog.Println("The number of bytes are:", bytes)
	return nil
}

func (m *Miner) callGetExistingChain(peerMiner string, fileListenAddr string, doneTransfer chan string, errTransfer chan error, prepTransfer chan bool) error {
	var getChainResp GetExistingChainResp
	peerRpcClient := m.PeersList[peerMiner]
	err := peerRpcClient.Call("Miner.GetExistingChainFromPeer", GetExistingChainArgs{fileListenAddr}, &getChainResp)
	if err != nil {
		infoLog.Println("Error from RPC Miner.GetExistingChainFromPeer", err)
		return err
	} else {
		prepTransfer <- true
		infoLog.Println("Got existing chain from peer: ", peerMiner)

		chainFileToValidate := <-doneTransfer
		// We want to verify the entire the entire chain on joins and rejoins -
		// including forks - to ensure integreity of the chain
		infoLog.Println("JOIN/REJOIN PROTOCOL: validating entire existing chain from peer")
		lastValidatedBlock, isValid, errFromValidate := m.validateExistingChainFromFile(chainFileToValidate)

		if errFromValidate == nil && isValid && lastValidatedBlock != nil {
			infoLog.Println("Chain from peer is valid!", peerMiner)
			os.Remove(OUTPUT_DIR + m.ChainStorageFile)                    // remove existing chain file
			os.Rename(chainFileToValidate, OUTPUT_DIR+m.ChainStorageFile) // rename temp file as new storage file
			infoLog.Println("LAST VALIDATED BLOCK", lastValidatedBlock)
			m.createNewMiningBlock(*lastValidatedBlock) // create new block based on last mined block
			return nil
		} else if !isValid {
			os.Remove(chainFileToValidate) // remove temp file if invalid
			return ErrInvalidChain
		} else {
			os.Remove(chainFileToValidate) // rmove temp file if error
			return err
		}
	}
}

func (m *Miner) startFileTransferServer(listenAddr string, doneTransfer chan string, errTransfer chan error, prepTransfer chan bool) {
	infoLog.Println("start listening for chain file transfers")
	server, err := net.Listen("tcp", listenAddr)
	if err != nil {
		infoLog.Println("There was an err starting the file transfer server", err)
		errTransfer <- ErrStartFileServer
	}
	for { // continuousuly accept connections in case of retries
		conn, err := server.Accept() // waits until connection dialed from peer
		if err != nil {
			infoLog.Println("There was an err with the file transfer connection", err)
			errTransfer <- err
		}
		// wait until transfer is done
		m.transferBlockchainFile(conn, doneTransfer, errTransfer, prepTransfer)
	}
}

func (m *Miner) transferBlockchainFile(conn net.Conn, doneTransfer chan string, errTransfer chan error, prepTransfer chan bool) {
	<-prepTransfer
	file, err := ioutil.TempFile(OUTPUT_DIR, "peer_blockchain_to_be_validated")
	if err != nil {
		infoLog.Println("Failed to create temp file to transfer blockchain", err)
		errTransfer <- err
		return
	}
	infoLog.Println("Created temp file to validate blockchain from peer:", file.Name())
	bytes, err := io.Copy(file, conn)
	if err != nil {
		infoLog.Println("Failed to copy from connection to temp file", err)
		errTransfer <- err
		return
	}
	infoLog.Println("The number of bytes are:", bytes)
	conn.Close() // close connection
	doneTransfer <- file.Name()
}

func (m *Miner) validateExistingChainFromFile(filepath string) (*MiningBlock, bool, error) {
	src, err := os.Open(filepath)
	infoLog.Println("validating file at path:", filepath)
	if err != nil {
		return nil, false, err
	}
	defer src.Close()
	// read line by line in case file large
	scanner := bufio.NewScanner(src)
	// Scan() reads next line and returns false when reached end or error
	var blockFromStorage *MiningBlock
	largestSeqNumSoFar := 0
	var lastValidatedBlock *MiningBlock // LAST BLOCK ON LONGEST CHAIN (LARGEST SEQNUM)
	for scanner.Scan() {
		// process the line
		blockLineAsBytes := scanner.Bytes()
		blockFromStorage = new(MiningBlock)
		err = m.unmarshalBlock(blockLineAsBytes, blockFromStorage)
		if err != nil {
			infoLog.Println("error unmarshalling")
			return nil, false, err
		}

		if !m.validateBlock(blockFromStorage) {
			return nil, false, nil
		}
		m.generateBlockLedger(*blockFromStorage)
		if blockFromStorage.SequenceNum >= largestSeqNumSoFar {
			lastValidatedBlock = blockFromStorage
			largestSeqNumSoFar = blockFromStorage.SequenceNum
		}
	}
	infoLog.Println("FINAL LEDGER BEFORE DONE JOINING:", m.Ledger)
	m.CurrLedger = copyMap(m.Ledger[lastValidatedBlock.CurrentHash]) // is not handled in generateBlockLedger, so we need this here
	return lastValidatedBlock, true, scanner.Err()                   // check if Scan() finished because of error or because it reached end of file
}

func (m *Miner) unmarshalBlock(data []byte, block *MiningBlock) error {
	err := json.Unmarshal(data, block)
	if err != nil {
		return err
	}
	if block.Transactions == nil {
		infoLog.Println("Creating empty list of transactions for unmarshal")
		block.Transactions = []Transaction{}
	}
	return nil
}

func (m *Miner) compareBlocks(a, b MiningBlock) bool {
	return a.SequenceNum == b.SequenceNum &&
		a.MinerPublicKey == b.MinerPublicKey &&
		a.Nonce == b.Nonce &&
		a.PrevHash == b.PrevHash &&
		a.CurrentHash == b.CurrentHash &&
		m.compareTransactionsSlices(a.Transactions, b.Transactions)
}

func (m *Miner) compareTransactionsSlices(a, b []Transaction) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// IsEmpty: check if stack is empty
func IsEmpty(stack [][]string) bool {
	return len(stack) == 0
}

// Push a new value onto the stack
func Push(stack [][]string, blockTweets []string) [][]string {
	return append(stack, blockTweets) // Simply append the new value to the end of the stack
}

// Remove and return top element of stack. Return false if stack is empty.
func Pop(stack [][]string) ([]string, bool) {
	if IsEmpty(stack) {
		return nil, false
	} else {
		index := len(stack) - 1   // Get the index of the top most element.
		element := (stack)[index] // Index into the slice and obtain the element.
		stack = (stack)[:index]   // Remove it from the stack by slicing it off.
		return element, true
	}
}
