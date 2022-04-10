/*

This fcheck library is used from part of our group's A3 solution:
https://github.students.cs.ubc.ca/CPSC416-2021W-T2/cpsc416_a3_joshc822_alika_ianmah/blob/main/fcheck/fcheck.go

*/

/*

This package specifies the API to the failure checking library to be
used in assignment 2 of UBC CS 416 2021W2.

You are *not* allowed to change the API below. For example, you can
modify this file by adding an implementation to Stop, but you cannot
change its API.

*/

package fchecker

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type Fcheck struct {
	ackQuit       chan int
	hbQuit        chan int
	hbHandlerQuit chan int
	hbMap         map[uint64]time.Time
	notifyChannel chan FailureDetected
	lock          sync.Mutex
	mapLock       sync.Mutex
	avgRTT        float64
	lostMsg       uint8
	isActive      bool
	ackconn       *net.UDPConn
	hbconn        *net.UDPConn
}

////////////////////////////////////////////////////// DATA
// Define the message types fchecker has to use to communicate to other
// fchecker instances. We use Go's type declarations for this:
// https://golang.org/ref/spec#Type_declarations

// Heartbeat message.
type HBeatMessage struct {
	EpochNonce uint64 // Identifies this fchecker instance/epoch.
	SeqNum     uint64 // Unique for each heartbeat in an epoch.
}

// An ack message; response to a heartbeat.
type AckMessage struct {
	HBEatEpochNonce uint64 // Copy of what was received in the heartbeat.
	HBEatSeqNum     uint64 // Copy of what was received in the heartbeat.
}

// Notification of a failure, signal back to the client using this
// library.
type FailureDetected struct {
	UDPIpPort string    // The RemoteIP:RemotePort of the failed node.
	Timestamp time.Time // The time when the failure was detected.
}

////////////////////////////////////////////////////// API

type StartStruct struct {
	AckLocalIPAckLocalPort       string
	EpochNonce                   uint64
	HBeatLocalIPHBeatLocalPort   string
	HBeatRemoteIPHBeatRemotePort string
	LostMsgThresh                uint8
}

// Constructor
func NewFcheck() *Fcheck {
	return &Fcheck{}
}

// Starts the fcheck library.

func (f *Fcheck) Start(arg StartStruct) (notifyCh <-chan FailureDetected, err error) {
	if f.isActive {
		return nil, errors.New("Call Stop before calling Start again")
	}
	f.ackQuit = make(chan int, 1)
	f.hbQuit = make(chan int, 1)
	f.hbHandlerQuit = make(chan int, 1)

	// Represent RTT in nanoseconds to accomodate super low latency
	f.avgRTT = 3 * 1000 * 1000 * 1000

	// Check if we want to activate listen mode
	if arg.AckLocalIPAckLocalPort != "" {
		ackladdr, err := net.ResolveUDPAddr("udp", arg.AckLocalIPAckLocalPort)
		if err != nil {
			return nil, err
		}
		f.ackconn, err = net.ListenUDP("udp", ackladdr)
		if err != nil {
			return nil, err
		}
		// Listening for HeartBeats and Replying
		go f.ReceiveHeartbeat(f.ackconn)
		f.isActive = true
	}

	// If we are in listen only mode, return immediately
	if arg.HBeatLocalIPHBeatLocalPort == "" {
		return
	}

	hbladdr, err := net.ResolveUDPAddr("udp", arg.HBeatLocalIPHBeatLocalPort)
	if err != nil {
		return
	}
	hbraddr, err := net.ResolveUDPAddr("udp", arg.HBeatRemoteIPHBeatRemotePort)
	if err != nil {
		return
	}

	f.hbconn, err = net.DialUDP("udp", hbladdr, hbraddr)
	if err != nil {
		return
	}
	f.hbMap = make(map[uint64]time.Time)
	f.notifyChannel = make(chan FailureDetected, 1)

	// Monitor Node
	go f.SendHeartbeat(f.hbconn, arg)
	go f.ReceiveAck(f.hbconn, arg)
	f.isActive = true

	return f.notifyChannel, nil
}

func (f *Fcheck) SendHeartbeat(conn net.Conn, arg StartStruct) {
	var seqNum uint64 = 0
	f.lock.Lock()
	f.lostMsg = 0
	f.lock.Unlock()
	for {
		seqNum++
		select {
		case <-f.hbQuit:
			return
			// Added 15 Milliseconds to account for super low latency on ugrad machines
		case <-time.After(time.Duration(f.avgRTT) + 15*time.Millisecond):
			if f.lostMsg >= arg.LostMsgThresh {
				f.ackQuit <- 1
				f.notifyChannel <- FailureDetected{
					UDPIpPort: arg.HBeatRemoteIPHBeatRemotePort,
					Timestamp: time.Now(),
				}
				f.hbconn.Close()
				return
			}
			var data bytes.Buffer
			hbeat := HBeatMessage{
				EpochNonce: arg.EpochNonce,
				SeqNum:     seqNum,
			}
			err := gob.NewEncoder(&data).Encode(hbeat)
			if err != nil {
				fmt.Println("Error encoding Heartbeat: " + err.Error())
			}
			_, err = conn.Write(data.Bytes())
			if err != nil {
				fmt.Println("Error sending Heartbeat: " + err.Error())
			}
			f.mapLock.Lock()
			f.hbMap[hbeat.SeqNum] = time.Now()
			f.mapLock.Unlock()
			f.lock.Lock()
			f.lostMsg++
			f.lock.Unlock()
		}
	}
}

func (f *Fcheck) SendAck(conn *net.UDPConn, addr net.Addr, hbeat HBeatMessage) {
	var data bytes.Buffer
	ack := AckMessage{
		HBEatEpochNonce: hbeat.EpochNonce,
		HBEatSeqNum:     hbeat.SeqNum,
	}
	err := gob.NewEncoder(&data).Encode(ack)
	if err != nil {
		// fmt.Println(err)
	}
	_, err = conn.WriteTo(data.Bytes(), addr)
	if err != nil {
		fmt.Println("Error sending ACK to " + addr.String() + ": " + err.Error())
	}
}

// Receives Heartbeats and sends ack to the appropriate node
func (f *Fcheck) ReceiveHeartbeat(conn *net.UDPConn) {
	for {
		select {
		case <-f.hbHandlerQuit:
			return
		default:
			var hbeat HBeatMessage
			var data bytes.Buffer
			buf := make([]byte, 1024)
			decoder := gob.NewDecoder(&data)
			n, remoteaddr, err := conn.ReadFrom(buf)
			if err != nil {
				// fmt.Println("Error reading: " + err.Error())
				continue
			}

			data.Write(buf[:n])
			err = decoder.Decode(&hbeat)
			if err != nil {
				// fmt.Println("Error decoding: " + err.Error())
				continue
			}
			f.SendAck(conn, remoteaddr, hbeat)
		}
	}
}

func (f *Fcheck) ReceiveAck(conn net.Conn, arg StartStruct) {
	// update RTT, remove seq num from map
	for {
		select {
		case <-f.ackQuit:
			return
		default:
			var ack AckMessage
			var data bytes.Buffer
			buf := make([]byte, 1024)
			decoder := gob.NewDecoder(&data)
			n, err := conn.Read(buf)
			if err != nil {
				continue
			}

			data.Write(buf[:n])
			err = decoder.Decode(&ack)
			if err != nil {
				continue
			}
			if ack.HBEatEpochNonce == arg.EpochNonce {
				f.lock.Lock()
				f.lostMsg = 0
				f.lock.Unlock()
				f.mapLock.Lock()
				val, prs := f.hbMap[ack.HBEatSeqNum]
				f.mapLock.Unlock()
				if prs {
					// RTT calculation + delete
					duration := time.Since(val)
					f.avgRTT = (float64(duration) + f.avgRTT) / 2
					f.mapLock.Lock()
					delete(f.hbMap, ack.HBEatSeqNum)
					f.mapLock.Unlock()
				}
			}
		}
	}
}

// Tells the library to stop monitoring/responding acks.
func (f *Fcheck) Stop() {
	if !f.isActive {
		return
	}
	f.ackQuit <- 1
	f.hbQuit <- 1
	f.hbHandlerQuit <- 1

	f.hbconn.Close()
	f.ackconn.Close()
	f.isActive = false
}
