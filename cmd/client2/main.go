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
const MINER_ADDRESS = "127.0.0.1:6982"

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

	tweetsToPost := [3]string{"Client 2 says: hello world", "Client 2 says: tweeting", "Client 2 says: 2 more for fun"} // Intialized with values
	for i := 0; i < len(tweetsToPost); i++ {
		err := client.Post(tweetsToPost[i])
		if err != nil {
			log.Println("Failed to POST tweet:", err)
		} else {
			result := <-notifCh
			log.Println("POST SENT:", result)
		}
		time.Sleep(3 * time.Second)
	}
}
