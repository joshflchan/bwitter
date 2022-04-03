package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"cs.ubc.ca/cpsc416/p2/bwitter/bwitter"
)

type MinerConfig struct {
	CoordAddress     string
	MinerListenAddr  string
	ExpectedNumPeers uint64
	ChainStorageFile string
	GenesisBlock     bwitter.MiningBlock
}

func main() {
	// NOTE: MINERID in the config should be unique from other miners, maybe we use public key maybe something else
	minerConfig := ReadConfig("config/miner_config.json")

	miner := bwitter.NewMiner()

	err := miner.Start(minerConfig.CoordAddress, minerConfig.MinerListenAddr, minerConfig.ExpectedNumPeers, minerConfig.ChainStorageFile, minerConfig.GenesisBlock)
	CheckErr(err, "unable to start")
}

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
