package main

import (
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	"cs.ubc.ca/cpsc416/p2/bwitter/bweethlib"
)

const MINER_ID = "1"
const MINER_ADDRESS = "127.0.0.1:6508"
const NUM_TWEETS_TO_POST = 5

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

	getTweetsFlag := flag.String("get", "", "command to view tweets")
	flag.Parse()
	if *getTweetsFlag == "tweets" {
		client.GetTweets()
	} else {
		log.Printf("Posting %v tweets\n", NUM_TWEETS_TO_POST)
		for i := 0; i < NUM_TWEETS_TO_POST; i++ {
			err := client.Post(fmt.Sprintf("Client 4 says: tweet #%v...", i+1))
			if err != nil {
				log.Println("Failed to POST tweet:", err)
			} else {
				result := <-notifCh
				log.Println("POST SENT:", result)
			}
			time.Sleep(3 * time.Second)
		}
	}
}
