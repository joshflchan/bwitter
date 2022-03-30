package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"io/ioutil"
	"log"
)

// function to generate a private public keypair

// usage:
// go run cmd/gen/main.go -filename miner1

// https://stackoverflow.com/questions/64104586/use-golang-to-get-rsa-key-the-same-way-openssl-genrsa
func main() {
	filename := flag.String("filename", "key", "")
	flag.Parse()

	// generate key
	key, err := rsa.GenerateKey(rand.Reader, 256)
	if err != nil {
		log.Printf("Cannot generate RSA key\n")
		return
	}

	// Encode private key to PKCS#1 ASN.1 PEM.
	keyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		},
	)

	// Write private key to file.
	if err := ioutil.WriteFile(*filename+".rsa", keyPEM, 0700); err != nil {
		panic(err)
	}

	// pub := key.Public()
	// // Encode public key to PKCS#1 ASN.1 PEM.
	// pubPEM := pem.EncodeToMemory(
	// 	&pem.Block{
	// 		Type:  "RSA PUBLIC KEY",
	// 		Bytes: x509.MarshalPKCS1PublicKey(pub.(*rsa.PublicKey)),
	// 	},
	// )
	// // Write public key to file.
	// if err := ioutil.WriteFile(*filename+".rsa.pub", pubPEM, 0755); err != nil {
	// 	panic(err)
	// }
}
