package bwitter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
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
}

type MinerConfig struct {
	CoordAddress string
}

type Miner struct {
	MinerConfig *MinerConfig
}

var coordClient *rpc.Client
var peersList []string
var peerFailed chan string

func (m *Miner) Start() error {

	err := rpc.Register(m)
	CheckErr(err, "Failed to register Miner")

	m.MinerConfig = ReadConfig("../config/miner_config.json")
	coordClient, err = rpc.Dial("tcp", m.MinerConfig.CoordAddress)
	CheckErr(err, "Failed to establish connection between Miner and Coord")

	go maintainPeersList()

	return errors.New("Not implemented")
}

func maintainPeersList() {

	peerFailed = make(chan string)

	for {
		select {
		case msg := <-peerFailed:
			fmt.Println(msg)
			// case where peer has failed
			// remove the failed peer from peersList
			// request new peers from Coord

		default:
			if len(peersList) < 5 {
				var CoordGetPeerResponse CoordGetPeerResponse

				err := coordClient.Call("GetPeers", nil, &CoordGetPeerResponse)
				if err != nil {
					//TODO: whatdo
				}

				// get all addresses in response and replace the peerslist
				// TODO: do we want to request a whole new set of peers each time, or do we want to only request a specific number?
			}
		}
	}

}

func (m *Miner) Post(postArgs *PostArgs, response *PostResponse) error {

	return errors.New("Not implemented")
}

// The two below functions have been taken from a3
func ReadConfig(filepath string) *MinerConfig {
	configFile := filepath
	configData, err := ioutil.ReadFile(configFile)
	CheckErr(err, "reading config file")

	config := new(MinerConfig)
	err = json.Unmarshal(configData, config)
	CheckErr(err, "parsing config data")

	return config
}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
