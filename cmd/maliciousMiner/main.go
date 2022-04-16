package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	bwitter "cs.ubc.ca/cpsc416/p2/bwitter/demo"
	"cs.ubc.ca/cpsc416/p2/bwitter/util"
)

type MinerConfig struct {
	MinerPublicKeyFile string
	CoordAddress       string
	MinerListenAddr    string
	ExpectedNumPeers   uint64
	ChainStorageFile   string
	GenesisBlock       bwitter.MiningBlock
	RetryPeerThreshold uint8
}

func main() {
	config := flag.String("config", "", "path to config file for miner")
	flag.Parse()
	if *config == "" {
		err := errors.New("no config file passed. please use flag --config [filepath]")
		CheckErr(err, "config filepath error: %v\n", err)
	}

	minerConfig := ReadConfig(*config)
	parsedPublicKey, err := util.ParsePublicKey(minerConfig.MinerPublicKeyFile)
	if err != nil {
		CheckErr(err, "unable to parse public key from file: %v\n", err)
	}

	miner := bwitter.NewMiner()

	startErr := miner.Start(parsedPublicKey, minerConfig.CoordAddress, minerConfig.MinerListenAddr, minerConfig.ExpectedNumPeers, minerConfig.ChainStorageFile, minerConfig.GenesisBlock, minerConfig.RetryPeerThreshold)
	CheckErr(startErr, "unable to start: %v\n", startErr)
}

func ReadConfig(filepath string) *MinerConfig {
	configFile := filepath
	configData, err := ioutil.ReadFile(configFile)
	CheckErr(err, "reading config file: %v\n", err)

	config := new(MinerConfig)
	err = json.Unmarshal(configData, config)
	CheckErr(err, "parsing config data: %v\n", err)

	return config
}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
