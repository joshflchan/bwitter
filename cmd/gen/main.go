package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"io/ioutil"
	"log"
	"os"
)

// function to generate a private public keypair

// usage:
// go run cmd/gen/main.go --filename miner1

const KEYS_DIR = "keys/"

// https://stackoverflow.com/questions/64104586/use-golang-to-get-rsa-key-the-same-way-openssl-genrsa
func main() {
	filename := flag.String("filename", "key", "")
	flag.Parse()

	if _, err := os.Stat(KEYS_DIR); os.IsNotExist(err) {
		if err := os.Mkdir(KEYS_DIR, os.ModePerm); err != nil {
			log.Printf("Unable to create dir ./%v: %v\n", KEYS_DIR, err)
			return
		}
	}

	// generate key
	key, err := rsa.GenerateKey(rand.Reader, 1024)
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
	if err := ioutil.WriteFile(KEYS_DIR+*filename+".rsa", keyPEM, 0700); err != nil {
		panic(err)
	}

	pub := key.Public()
	// Encode public key to PKCS#1 ASN.1 PEM.
	pubPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: x509.MarshalPKCS1PublicKey(pub.(*rsa.PublicKey)),
		},
	)

	// Write public key to file.
	if err := ioutil.WriteFile(KEYS_DIR+*filename+".rsa.pub", pubPEM, 0755); err != nil {
		panic(err)
	}
}
