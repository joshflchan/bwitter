package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"cs.ubc.ca/cpsc416/p2/bwitter/bwitter"
)

type CoordConfig struct {
	CoordAddress           string
	CoordRPCListenPort     string
	MinNumNeighbors        int16
	LostHeartbeatThreshold int8
}

func main() {
	coordConfig := ReadConfig("config/coord_config.json")

	err := bwitter.StartCoord(coordConfig.CoordAddress, coordConfig.CoordRPCListenPort, coordConfig.MinNumNeighbors, coordConfig.LostHeartbeatThreshold)
	CheckErr(err, "unable to start coord")
}

func ReadConfig(filepath string) *CoordConfig {
	configFile := filepath
	configData, err := ioutil.ReadFile(configFile)
	CheckErr(err, "reading coord config file")

	config := new(CoordConfig)
	err = json.Unmarshal(configData, config)
	CheckErr(err, "parsing coord config data")

	return config
}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
