package bwitter

import (
	"fmt"
	"math/rand"
	"net"
	"net/rpc"
	"reflect"
	"time"
)

type Coord struct {
	MinerPool map[string]string

	MinNumNeighbors    int16
	CoordListenAddress string
}

func NewCoord() *Coord {
	return &Coord{}
}

func (c *Coord) Start(CoordListenAddress string, MinNumNeighbors int16) error {
	rand.Seed(time.Now().UTC().UnixNano())
	c.CoordListenAddress = CoordListenAddress
	c.MinNumNeighbors = MinNumNeighbors

	err := rpc.Register(c)
	if err != nil {
		fmt.Println("failed to register")
		return err
	}
	coordListener, err := net.Listen("tcp", c.CoordListenAddress)
	if err != nil {
		return err
	}
	go rpc.Accept(coordListener)

	return nil
}

// Coord.GetPeers
type CoordGetPeersArgs struct {
	IncomingMinerAddr string
}
type CoordGetPeersResponse struct {
	NeighborAddrs []string
}

func (c *Coord) GetPeers(args *CoordGetPeersArgs, response *CoordGetPeersResponse) error {
	fmt.Println(args.IncomingMinerAddr + ": Coord.GetPeers received")
	addrsToReturn := make(map[string]bool)
	keys := reflect.ValueOf(c.MinerPool).MapKeys()
	for i := int16(0); i < c.MinNumNeighbors && i < int16(len(c.MinerPool)); i++ {
		randomIndex := rand.Intn(len(c.MinerPool))
		randomMinerAddr := keys[randomIndex].Interface().(string)

		if randomMinerAddr == args.IncomingMinerAddr {
			fmt.Println(args.IncomingMinerAddr + ": randomMinerAddr is same as requester")
			continue
		} else if addrsToReturn[randomMinerAddr] {
			fmt.Println(args.IncomingMinerAddr + ": randomMinerAddr has already been selected")
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
	fmt.Println(args.IncomingMinerAddr + ": Coord.NotifyJoin received")

	c.MinerPool[args.IncomingMinerAddr] = args.MinerFcheckAddr

	go fcheckMiner(c, args.IncomingMinerAddr, args.MinerFcheckAddr)
	return nil
}

func fcheckMiner(c *Coord, minerListenAddr string, minerFcheckAddr string) {
	fmt.Println(minerListenAddr + ": beginning fcheck to " + minerFcheckAddr)
	// TODO
}
