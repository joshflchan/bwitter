package main

import (
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"log"
	"time"

	"cs.ubc.ca/cpsc416/p2/bwitter/bweethlib"
)

const MINER_ID = "2"
const MINER_ADDRESS = "127.0.0.1:6972"

func main() {
	pemString, err := ioutil.ReadFile("keys/miner" + MINER_ID + ".rsa")
	if err != nil {
		log.Println(err)
		return
	}
	block, _ := pem.Decode(pemString)
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Println(err)
	}

	log.Println(key.N)

	client := bweethlib.NewBweeth()
	notifCh, err := client.Start(key, MINER_ADDRESS, 3)
	if err != nil {
		log.Println(err)
		return
	}

	client.Post("Client 2 says: hello world")
	time.Sleep(3 * time.Second)
	client.Post("Client 2 says: tweeting")
	time.Sleep(5 * time.Second)
	client.Post("Client 2 says: 1 more for fun")

	for i := 0; i < 3; i++ {
		result := <-notifCh
		log.Println("NOTIFYCH RESULT:", result)
	}
}
