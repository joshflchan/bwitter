package bwitter

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"math/rand"
	"net"
	"net/rpc"
	"os"
	"reflect"
	"time"
)

type Coord struct {
	MinerPool map[string]bool

	CoordAddress           string
	CoordRPCListenAddress  string
	MinNumNeighbors        int16
	lostHeartbeatThreshold int8
}

func NewCoord() *Coord {
	return &Coord{}
}

func StartCoord(coordAddress string, coordRPCListenPort string, minNumNeighbors int16, lostHeartbeatThreshold int8) error {
	fmt.Println("Coord.Start: begins")
	c := NewCoord()
	rand.Seed(time.Now().UTC().UnixNano())

	c.MinerPool = make(map[string]bool)
	c.CoordAddress = coordAddress
	c.CoordRPCListenAddress = coordAddress + ":" + coordRPCListenPort
	c.MinNumNeighbors = minNumNeighbors
	c.lostHeartbeatThreshold = lostHeartbeatThreshold

	fmt.Println("Coord.Start: registering Coord for RPC")
	err := rpc.Register(c)
	if err != nil {
		fmt.Println("failed to register")
		return err
	}

	fmt.Println("Coord.Start: setting up RPC listener")
	coordRPCListener, err := net.Listen("tcp", c.CoordRPCListenAddress)
	if err != nil {
		fmt.Println("failed to listen RPC")
		return err
	}
	go rpc.Accept(coordRPCListener)

	fmt.Println("Coord.Start: finished")
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

	minerFcheckAddr, err := net.ResolveUDPAddr("udp", args.MinerFcheckAddr)
	if err != nil {
		fmt.Println(args.IncomingMinerAddr + ": failed to resolve minerFcheckAddr")
		return err
	}

	conn, err := c.createUDPConnOnRandomPort()
	if err != nil {
		fmt.Println(args.IncomingMinerAddr + ": failed to create coordFcheckAddr on random port")
		return err
	}

	// Add miner to MinerPool
	c.MinerPool[args.IncomingMinerAddr] = true
	go c.fcheckMiner(conn, args.IncomingMinerAddr, minerFcheckAddr)
	return nil
}

// Heartbeat message.
type HBeatMessage struct {
	MinerListenAddr string // Identifies this fchecker instance/epoch.
	SeqNum          uint64 // Unique for each heartbeat in an epoch.
}

// An ack message; response to a heartbeat.
type AckMessage struct {
	MinerListenAddr string // Copy of what was received in the heartbeat.
	HBEatSeqNum     uint64 // Copy of what was received in the heartbeat.
}

func (c *Coord) createUDPConnOnRandomPort() (*net.UDPConn, error) {
	addressWithRandomPort := &net.UDPAddr{
		Port: 0, // find random open port
		IP:   net.ParseIP(c.CoordAddress),
	}
	fcheckConn, err := net.ListenUDP("udp", addressWithRandomPort)
	if err != nil {
		fmt.Println("failed to listen UDP")
		return nil, err
	}
	return fcheckConn, nil
}

func (c *Coord) fcheckMiner(conn *net.UDPConn, minerListenAddr string, minerFcheckAddr *net.UDPAddr) {
	fmt.Println(minerListenAddr + ": beginning fcheck to " + minerFcheckAddr.IP.String())
	defer fmt.Println(minerListenAddr + ": stopping fcheck to " + minerFcheckAddr.IP.String())
	defer conn.Close()

	heartbeatSeqNum := uint64(0)
	retries := int8(0)

	hbWriteTimeMap := map[uint64]time.Time{}
	lastRttNs := int64(3 * 1e9) // 3 seconds from nanoseconds
	timeoutDuration := time.Duration(lastRttNs)

	for {
		heartbeatMessage := HBeatMessage{
			MinerListenAddr: minerListenAddr,
			SeqNum:          heartbeatSeqNum,
		}
		encodedHeartbeat := encode(heartbeatMessage)

		fmt.Fprintln(os.Stdout, "Sending a heartbeat message: ", heartbeatMessage)
		_, werr := conn.WriteToUDP(encodedHeartbeat, minerFcheckAddr)
		if werr != nil {
			fmt.Fprintln(os.Stderr, "Write error occured: ", werr)
			return //notifyCh, errors.New("unable to write hbeat message")
		}
		hbWriteTimeMap[heartbeatSeqNum] = time.Now()
		heartbeatSeqNum++

		for {
			recvBuf := make([]byte, 1024)
			readStartTime := time.Now()
			timeout := readStartTime.Add(timeoutDuration)
			conn.SetReadDeadline(timeout)
			len, _, err := conn.ReadFromUDP(recvBuf)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				retries++
				fmt.Fprintln(os.Stderr, "Reached the timeout with retries: ", timeoutDuration.Seconds(), retries)
				if retries == c.lostHeartbeatThreshold {
					fmt.Fprintln(os.Stderr, "Failure detected")
					go c.onMinerFailure(minerListenAddr)
					return
				}
				// If timed out and not met lost msg threshold, re-send heartbeat
				break
			}
			readTimeUnixNs := time.Now().UnixNano()
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error reading Ack, retrying read: ", err)
				actualReadTime := readStartTime.UnixNano() - readTimeUnixNs
				timeoutDuration = time.Duration(timeoutDuration.Nanoseconds() - actualReadTime)
				continue
			}
			ackMessage, err := decodeAckMessage(recvBuf, len)
			if err != nil || ackMessage.MinerListenAddr != minerListenAddr {
				fmt.Fprintln(os.Stderr, "Bad Ack, retrying read")
				actualReadTime := readStartTime.UnixNano() - readTimeUnixNs
				timeoutDuration = time.Duration(timeoutDuration.Nanoseconds() - actualReadTime)
				continue
			}

			if writeTime, ok := hbWriteTimeMap[ackMessage.HBEatSeqNum]; ok {
				// got a valid ack to a heartbeat
				// calculate rtt
				rtt := readTimeUnixNs - writeTime.UnixNano()
				timeoutDuration = time.Duration((lastRttNs + rtt) / 2)
				fmt.Println("Successful RTT, new timeout: ", timeoutDuration.Seconds())
				delete(hbWriteTimeMap, ackMessage.HBEatSeqNum)
				retries = 0
				lastRttNs = rtt
				// ready to send new heartbeats
				break
			}
			fmt.Println("Seq num does not exist in map, retrying")
		}
	}
}

func (c *Coord) onMinerFailure(minerListenAddr string) {
	fmt.Println(minerListenAddr + ": removing from pool")
	delete(c.MinerPool, minerListenAddr)
}

func decodeAckMessage(buf []byte, len int) (AckMessage, error) {
	var decoded AckMessage
	err := gob.NewDecoder(bytes.NewBuffer(buf[0:len])).Decode(&decoded)
	if err != nil {
		return AckMessage{}, err
	}
	return decoded, nil
}

func encode(e interface{}) []byte {
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(e)
	return buf.Bytes()
}
