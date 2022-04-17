package main

import (
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"log"
	"time"

	"cs.ubc.ca/cpsc416/p2/bwitter/bweethlib"
)

const MINER_ID = "1"
const MINER_ADDRESS = "127.0.0.1:6505"

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

	client.Post("Client 1 says: hello world")
	time.Sleep(3 * time.Second)
	client.Post("Client 1 says: tweeting")
	time.Sleep(7 * time.Second)
	client.Post("Client 1 says: 1 more for fun")
	time.Sleep(10 * time.Second)
	log.Println("Getting Tweets")
	client.GetTweets()

	for i := 0; i < 3; i++ {
		result := <-notifCh
		log.Println("NOTIFYCH RESULT:", result)
	}
}
