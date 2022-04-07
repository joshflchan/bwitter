package bwitter

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/gob"
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
	Target             *big.Int
	MiningBlock        MiningBlock
	PeersList          map[string]*rpc.Client
	CoordClient        *rpc.Client
	PeerFailed         chan string
	ChainStorageFile   string
	broadcastChannel   chan MiningBlock
	BlocksSeen         map[string]bool // Hash of blocks seen

	TransactionsList []Transaction
}

type MiningBlock struct {
	SequenceNum    int
	MinerPublicKey string
	Transactions   []Transaction
	Nonce          int64
	PrevHash       string
	CurrentHash    string
}

type Transaction struct {
	Timestamp string
	Tweet     string
}

type GetExistingChainArgs struct {
	FileListenAddr string
}

type GetExistingChainResp struct {
}

const OUTPUT_DIR = "out/"

var (
	ErrInvalidChain    = errors.New("chain from peer was invalid")
	ErrStartFileServer = errors.New("failed to start file transfer server")
	infoLog            *log.Logger
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

	// TODO READ TARGET BITS FROM CONFIG, SETS DIFFICULTY
	// inspired by gochain
	m.TargetBits = 16
	m.Target = big.NewInt(1)
	m.Target.Lsh(m.Target, uint(256-m.TargetBits))

	m.MinerPublicKey = publicKey
	m.CoordAddress = coordAddress
	m.MinerListenAddr = minerListenAddr
	m.ExpectedNumPeers = expectedNumPeers
	m.PeersList = make(map[string]*rpc.Client)
	m.ChainStorageFile = chainStorageFile
	m.RetryPeerThreshold = retryPeerThreshold
	m.broadcastChannel = make(chan MiningBlock)
	m.BlocksSeen = make(map[string]bool)

	minerListener, err := net.Listen("tcp", m.MinerListenAddr)
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
	}

	for {
		rpc.Accept(minerListener)
	}
}

func (m *Miner) initialJoin(genesisBlock MiningBlock) error {
	// Get peers from Coord and add to peersList
	newRequestedPeers := m.callCoordGetPeers(m.ExpectedNumPeers)
	m.addNewMinerToPeersList(newRequestedPeers)
	// Maintain peersList
	go m.maintainPeersList()

	// TODO: Get entire blockchain from a peer
	fileListenAddr, err := util.GetAddressWithUnusedPort(m.MinerListenAddr)
	if err != nil {
		infoLog.Println(err)
		return err
	}
	doneTransfer := make(chan string, 1)
	errTransfer := make(chan error, 1)
	prepTransfer := make(chan bool, 1)
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
				err := m.callGetExistingChain(peerMiner, fileListenAddr, doneTransfer, errTransfer, prepTransfer)
				if err != nil {
					infoLog.Println("Error from RPC Miner.GetExistingChainFromPeer", err)
					if errors.Is(err, ErrInvalidChain) {
						infoLog.Println("Removing peer from PeerList since given chain is invalid")
						break
					} else if errors.Is(err, ErrStartFileServer) {
						return err
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
			break
		}
	}

	// Start fcheck to acknowledge heartbeats from Coord before notifying Coord of Join
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
			infoLog.Println("detected failed peer... removing")
			m.removeFailedMiner(failedClient)
			newRequestedPeers := m.callCoordGetPeers(1)
			m.addNewMinerToPeersList(newRequestedPeers)
		default: // continuously check for expected num peers to build robustness of network
			lenOfExistingPeerList := uint64(len(m.PeersList))
			if lenOfExistingPeerList < m.ExpectedNumPeers {
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
	return getPeersResponse.NeighborAddrs
}

func (m *Miner) addNewMinerToPeersList(newRequestedPeers []string) {
	for _, peer := range newRequestedPeers {
		if _, ok := m.PeersList[peer]; ok {
			continue
		} else {
			infoLog.Println("Adding new miner to peer list:", peer)
			peerConnection, err := rpc.Dial("tcp", peer)
			if err == nil {
				m.PeersList[peer] = peerConnection
			}
		}
	}
}

// RPC Call for client
func (m *Miner) Post(postArgs *util.PostArgs, response *util.PostResponse) error {
	infoLog.Println("POST msg received:", postArgs.MessageContents)
	msgContent := postArgs.MessageContents + postArgs.Timestamp

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
		infoLog.Println("Failed to verify signature for Post", err)
		return err
	}

	// if decryption successful, create Transaction and add to list
	transaction := Transaction{Timestamp: postArgs.Timestamp, Tweet: postArgs.MessageContents}
	// Not sure what this is for? Who uses that variable?
	m.TransactionsList = append(m.TransactionsList, transaction)

	// This is what we want so that it gets mined
	m.MiningBlock.Transactions = append(m.MiningBlock.Transactions, transaction)

	infoLog.Println("tx:", transaction)
	// propagate op [JOSH]

	return nil
}

// try a bunch of nonces on current block of transactions, as transactions change
// Assumes m.MiningBlock is set externally
func (m *Miner) mineBlock() {
	for {
		infoLog.Println("Mining block: ", m.MiningBlock)
		var block MiningBlock
		var hashInteger big.Int
		// is 32 necessary? maybe to chop off excess
		var hash [32]byte
		nonce := int64(0)
		for nonce < math.MaxInt64 {
			copier.CopyWithOption(&block, &m.MiningBlock, copier.Option{IgnoreEmpty: false, DeepCopy: true})
			block.Nonce = nonce
			blockBytes := convertBlockToBytes(block)
			if blockBytes != nil {
				hash = sha256.Sum256(blockBytes)
				hashInteger.SetBytes(hash[:])
				// this will be true if the hash computed has the first m.TargetBits as 0
				if hashInteger.Cmp(m.Target) == -1 {
					block.CurrentHash = hex.EncodeToString(hash[:])
					infoLog.Println("MINED BLOCK: ", block)
					break
				}

			}
			// unlock here?
			nonce++
		}
		// A) value is now in m.MiningBlock, maybe feed this to a channel that is waiting on it to broadcast to other nodes?
		m.createNewMiningBlock(block)
		m.writeNewBlockToStorage(block)
	}
}

func (m *Miner) getLastThresholdBlocksFromStorage(threshold int) ([]MiningBlock, error) {
	fileHandle, err := os.Open("out/" + m.ChainStorageFile)
	if err != nil {
		infoLog.Printf("The file ./out/"+m.ChainStorageFile+" does not exist: %v\n", err)
		return nil, err
	}
	defer fileHandle.Close()

	lastThresholdBlocks := make([]MiningBlock, threshold)

	line := ""
	var cursor int64 = 0
	stat, _ := fileHandle.Stat()
	filesize := stat.Size()
	var recvdLines int = 0
	for recvdLines < int(threshold) {
		cursor -= 1
		fileHandle.Seek(cursor, io.SeekEnd)

		char := make([]byte, 1)
		fileHandle.Read(char)

		if cursor != -1 && (char[0] == 10 || char[0] == 13) { // stop if we find a line
			recvdLines++
			err = json.Unmarshal([]byte(line), &lastThresholdBlocks[threshold-recvdLines])
			if err != nil {
				infoLog.Printf("Unable to unmarshal line %d from bottom of file\n", recvdLines)
				return nil, err
			}
			line = ""
			continue
		}

		line = fmt.Sprintf("%s%s", string(char), line) // there is more efficient way

		if cursor == -filesize { // stop if we are at the begining
			err = json.Unmarshal([]byte(line), &lastThresholdBlocks[threshold-recvdLines])
			if err != nil {
				infoLog.Println("Unable to unmarshal first line from bottom of file")
				return nil, err
			}
			break
		}
	}

	validLinesRecvd := threshold - recvdLines
	return lastThresholdBlocks[validLinesRecvd:], nil
}

func (m *Miner) writeNewBlockToStorage(minedBlock MiningBlock) {
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

// Comments for josh:
// You'd also need another function (probably goroutine) that receives broadcasted blocks from peers
// it would do the validation
// it would also update the block that is CURRENTLY being mined and see what transactions are missing
// then it would and write it to the txt file,

// this is goroutine that needs to be started somewhere
func (m *Miner) broadcastMinedBlock() {
	for {
		select {
		// case <-quitWrite:
		// 		return
		case block := <-m.broadcastChannel:
			fmt.Println(block)
			// trace.RecordAction(ClientMove(req)) thots thots?

			// iterate through list of peers, conn.Write
			// if peers are just addresses we would want conn.Dial first
			// _, err = conn.Write(data.Bytes())
			// if err != nil {
			// 	continue
			// }
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
	m.MiningBlock.SequenceNum = oldSeqNum + 1
	m.MiningBlock.PrevHash = prevHash
	m.MiningBlock.MinerPublicKey = m.MinerPublicKey
	copy(m.MiningBlock.Transactions, missingTransactions)
}

func convertBlockToBytes(block MiningBlock) []byte {
	var data bytes.Buffer
	enc := gob.NewEncoder(&data)
	err := enc.Encode(block)
	if err != nil {
		infoLog.Println(err)
		os.Exit(1)
		return nil
	}
	return data.Bytes()
}

// validate block has two parts
// 0)? check if this hash has been seen already, short circuit if so
// A) check proof of work hash actually corresponds to block
// B) check transactions make sense
func (m *Miner) validateBlock(block *MiningBlock) bool {
	_, ok := m.BlocksSeen[block.CurrentHash]
	if ok {
		// seen this block already, ignore
		return false
	}
	// we can mark as seen even if this block would be found invalid in the future
	m.BlocksSeen[block.CurrentHash] = true
	// call validatePow
	// call some function that checks transactions are valid using previous balances

	// if valid, return true
	return m.validatePoW(block)
}

// Do we also wanna check difficulty?
func (m *Miner) validatePoW(block *MiningBlock) bool {
	var computedHash [32]byte
	var computedHashInteger big.Int
	var givenHash [32]byte
	var givenHashInteger big.Int

	givenHashDecoded, _ := hex.DecodeString(block.CurrentHash)
	copy(givenHash[:], givenHashDecoded)
	block.CurrentHash = ""
	blockBytes := convertBlockToBytes(*block)
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
		return err
	} else {
		prepTransfer <- true
		infoLog.Println("Got existing chain from peer: ", peerMiner)
		select { // block until finish file transfer
		case chainFileToValidate := <-doneTransfer:
			lastValidatedBlock, isValid, err := m.validateExistingChainFromFile(chainFileToValidate)
			if err == nil && isValid {
				infoLog.Println("Chain from peer is valid!", peerMiner)
				os.Rename(chainFileToValidate, OUTPUT_DIR+m.ChainStorageFile) // rename temp file as new storage file
				m.createNewMiningBlock(*lastValidatedBlock)                   // create new block based on last mined block
				return nil
			} else if !isValid {
				os.Remove(chainFileToValidate) // remove temp file if invalid
				return ErrInvalidChain
			} else {
				os.Remove(chainFileToValidate) // rmove temp file if error
				return err
			}
		case err := <-errTransfer:
			return err
		}
	}
}

func (m *Miner) startFileTransferServer(listenAddr string, doneTransfer chan string, errTransfer chan error, prepTransfer chan bool) {
	infoLog.Println("start listening")
	server, err := net.Listen("tcp", listenAddr) // TODO: properly close connection
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
		// wait until trnasfer good
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
	// TODO: Perform validation on chain and store on permanent path from config
	src, err := os.Open(filepath)
	infoLog.Println("validating file at path:", filepath)
	if err != nil {
		return nil, false, err
	}
	defer src.Close()
	// read line by line in case file large
	scanner := bufio.NewScanner(src)
	// Scan() reads next line and returns false when reached end or error
	var blockToValidate *MiningBlock
	for scanner.Scan() {
		blockLineAsBytes := scanner.Bytes()
		// process the line
		blockToValidate = new(MiningBlock)
		err = json.Unmarshal(blockLineAsBytes, blockToValidate)
		if err != nil {
			return nil, false, err
		}
		if !m.validateBlock(blockToValidate) {
			return nil, false, nil
		}
	}
	lastValidatedBlock := blockToValidate
	return lastValidatedBlock, true, scanner.Err() // check if Scan() finished because of error or because it reached end of file
}
