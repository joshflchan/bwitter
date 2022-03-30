package main

import (
	"crypto/x509"
	"encoding/pem"
	"log"

	"cs.ubc.ca/cpsc416/p2/bwitter/bweethlib"
)

func main() {
	// Code for client to run, prolly smth smth read from config smth smth tweet this smth smth see all tweets

	// https://stackoverflow.com/questions/44230634/how-to-read-an-rsa-key-from-file
	pemString := `-----BEGIN RSA PRIVATE KEY-----
MIGqAgEAAiEAxMLwx+bHMvJcEZbnMB96sfhWQX5Z3oDopzAAjgF0ssUCAwEAAQIg
DBQHYc4J1lfITRAdWvfjuSN3OJawGZwyQohhqTWwfmECEQDWc4c/SnMOzO+MoErl
mj95AhEA6uIGKuh06Iojxn1ecvy+rQIQYaLrusccp2pqzi3Uq8CUkQIRAKo+9ZV4
M/Sw28ls6V6TD2kCEA2ALscJroe5Y/BNoFOJ8ao=
-----END RSA PRIVATE KEY-----`

	block, _ := pem.Decode([]byte(pemString))
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Println(err)
	}

	log.Println(key.N)

	client := bweethlib.NewBweeth()
	notifCh, err := client.Start("0x5318008", "127.0.0.1:1234", 3)
	if err != nil {
		log.Println(err)
	}

	for i := 0; i < 3; i++ {
		result := <-notifCh
		log.Println("NOTIFYCH RESULT:", result)
	}
}
