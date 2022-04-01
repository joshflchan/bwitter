package util

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
)

// addrToSplit: address used to generate random available port; if empty string, then uses local address of the caller
func GetAddressWithUnusedPort(addrToSplit string) (string, error) {
	var tempListener net.Listener
	var listenErr error
	if addrToSplit == "" { // uses local address of caller
		tempListener, listenErr = net.Listen("tcp", ":0")
	} else {
		addr, _, err := net.SplitHostPort(addrToSplit)
		if err != nil {
			// failed to split address
			return "", err
		}
		// otherwise uses given address in parameters
		tempListener, listenErr = net.Listen("tcp", addr+":0")
	}
	if listenErr != nil {
		// failed to generate random port
		return "", listenErr
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
