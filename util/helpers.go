package util

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
)

func GetAddressWithUnusedPort(addrToSplit string) (string, error) {
	// generate random port
	addr, _, err := net.SplitHostPort(addrToSplit)
	if err != nil {
		// failed to split address
		return "", err
	}
	tempListener, err := net.Listen("tcp", addr+":0")
	if err != nil {
		// failed to generate random port
		return "", err
	}
	availableAddr := tempListener.Addr().String()
	tempListener.Close()
	return availableAddr, nil
}

func ReadJSONConfig(filename string, config interface{}) error {
	configData, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	err = json.Unmarshal(configData, config)
	if err != nil {
		return err
	}
	return nil
}

func CheckErr(err error, errfmsg string, fargs ...interface{}) {
	if err != nil {
		fmt.Fprintf(os.Stderr, errfmsg, fargs...)
		os.Exit(1)
	}
}
