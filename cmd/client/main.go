package main

import (
	"log"

	"cs.ubc.ca/cpsc416/p2/bwitter/bweethlib"
)

func main() {
	// Code for client to run, prolly smth smth read from config smth smth tweet this smth smth see all tweets

	client := bweethlib.NewBweeth()
	notifCh, err := client.Start("0x8008153", "127.0.0.1:1234", 3)
	if err != nil {
		log.Println(err)
	}

	for i := 0; i < 3; i++ {
		result := <-notifCh
		log.Println("NOTIFYCH RESULT:", result)
	}
}
