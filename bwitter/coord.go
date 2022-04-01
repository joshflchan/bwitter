package bwitter

import (
	"log"
	"math/rand"
	"net"
	"net/rpc"
	"reflect"
	"time"

	fchecker "cs.ubc.ca/cpsc416/p2/bwitter/fcheck"
	"cs.ubc.ca/cpsc416/p2/bwitter/util"
)

type Coord struct {
	MinerPool map[string]bool

	CoordAddress           string
	CoordRPCListenAddress  string
	lostHeartbeatThreshold uint8
}

func NewCoord() *Coord {
	return &Coord{}
}

func StartCoord(coordAddress string, coordRPCListenPort string, minNumNeighbors int16, lostHeartbeatThreshold uint8) error {
	log.Println("Coord.Start: begins")
	c := NewCoord()
	rand.Seed(time.Now().UTC().UnixNano())

	c.MinerPool = make(map[string]bool)
	c.CoordAddress = coordAddress
	c.CoordRPCListenAddress = coordAddress + ":" + coordRPCListenPort
	c.lostHeartbeatThreshold = lostHeartbeatThreshold

	log.Println("Coord.Start: registering Coord for RPC")
	err := rpc.Register(c)
	if err != nil {
		log.Println("failed to register")
		return err
	}

	log.Println("Coord.Start: setting up RPC listener")
	coordRPCListener, err := net.Listen("tcp", c.CoordRPCListenAddress)
	if err != nil {
		log.Println(c.CoordRPCListenAddress)
		log.Println("failed to listen RPC")
		return err
	}
	log.Println("Coord.Start: finished")
	for {
		rpc.Accept(coordRPCListener)
	}
}

// Coord.GetPeers
type CoordGetPeersArgs struct {
	IncomingMinerAddr string
	ExpectedNumPeers  uint64
}
type CoordGetPeersResponse struct {
	NeighborAddrs []string
}

func (c *Coord) GetPeers(args *CoordGetPeersArgs, response *CoordGetPeersResponse) error {
	log.Println(args.IncomingMinerAddr + ": Coord.GetPeers received")

	addrsToReturn := make(map[string]bool)
	keys := reflect.ValueOf(c.MinerPool).MapKeys()
	for i := uint64(0); i < uint64(args.ExpectedNumPeers) && i < uint64(len(c.MinerPool)); i++ {
		randomIndex := rand.Intn(len(c.MinerPool))
		randomMinerAddr := keys[randomIndex].Interface().(string)

		if randomMinerAddr == args.IncomingMinerAddr {
			log.Println(args.IncomingMinerAddr + ": randomMinerAddr is same as requester")
			continue
		} else if addrsToReturn[randomMinerAddr] {
			log.Println(args.IncomingMinerAddr + ": randomMinerAddr has already been selected")
			continue
		}
		addrsToReturn[randomMinerAddr] = true
	}
	response.NeighborAddrs = make([]string, 0, len(addrsToReturn))
	for key := range addrsToReturn {
		response.NeighborAddrs = append(response.NeighborAddrs, key)
	}
	return nil
}

// Coord.NotifyJoin
type CoordNotifyJoinArgs struct {
	IncomingMinerAddr string
	MinerFcheckAddr   string
}
type CoordNotifyJoinResponse struct {
}

func (c *Coord) NotifyJoin(args *CoordNotifyJoinArgs, response *CoordNotifyJoinResponse) error {
	log.Println("JOIN PROTOCOL: Acknowledge miner join")
	log.Println(args.IncomingMinerAddr + ": Coord.NotifyJoin received")
	log.Println(args.MinerFcheckAddr + ": resolving MinerFcheckAddr")

	// Add miner to MinerPool
	log.Println(args.IncomingMinerAddr + ": adding miner to miner pool")
	c.MinerPool[args.IncomingMinerAddr] = true
	log.Println("Adding new miner to mining pool: ", c.MinerPool)

	err := c.startFcheck(args)
	if err != nil {
		log.Println("Failed to start FCheck in Coord to monitor miner with ID:", args.IncomingMinerAddr)
		return err
	}
	return nil
}

func (c *Coord) startFcheck(args *CoordNotifyJoinArgs) error {
	hBeatLocalAddr, err := util.GetAddressWithUnusedPort(c.CoordRPCListenAddress)
	if err != nil {
		log.Println("Failed to get random port for fcheck hBeatLocalAddr", err)
		return err
	}

	// Start Monitoring
	fcheckInstance := fchecker.NewFcheck()

	notifyCh, err := fcheckInstance.Start(
		fchecker.StartStruct{
			AckLocalIPAckLocalPort:       "",            // Ignore because we don't need the Coord to ACK heartbeats
			EpochNonce:                   rand.Uint64(), //TODO: maybe change to miner address
			HBeatLocalIPHBeatLocalPort:   hBeatLocalAddr,
			HBeatRemoteIPHBeatRemotePort: args.MinerFcheckAddr,
			LostMsgThresh:                c.lostHeartbeatThreshold,
		})
	if err != nil {
		log.Println("Failed to start fcheck", err)
		return err
	}

	go c.monitorMiner(args.IncomingMinerAddr, notifyCh)
	log.Println("Successfully started fcheck instance in coord for: ", args.IncomingMinerAddr)
	return nil
}

func (c *Coord) monitorMiner(minerAddr string, notifyCh <-chan fchecker.FailureDetected) {
	log.Println("MONITORING miner: ", minerAddr)
	noti := <-notifyCh
	log.Println(noti)
	log.Println("fcheck detected failed miner: ", minerAddr)
	c.onMinerFailure(minerAddr)
}

func (c *Coord) onMinerFailure(minerListenAddr string) {
	log.Println(minerListenAddr + ": removing from pool")
	delete(c.MinerPool, minerListenAddr)
	log.Println("Removed complete. New mining pool: ", c.MinerPool)
}
