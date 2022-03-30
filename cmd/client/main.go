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
MIICXQIBAAKBgQDGLihaWikFCevMq8unQNNOUrkNP4VVxEQDDWfBXWXBNvYJ+52m
nRTbWWmZIVSBkxMXd+t6NaqZsXJGaPrSYzxw4sAzg94OxbrnMa7RTItLZB5OCbJw
OejlzGsCmifbA1Fsi0aLBw5aGj4ljZh4k5MxwhEmMdY0ik384rK1gBVWJQIDAQAB
AoGAM0C6tOs+UoxHTE5dw+qS+12PeCqmXBD/Gd78p1h1OWvyY5CMLAvR2gycr7qb
9UrJFDeyUY/RiCAJEsaRn5mEhqQBoGvDGJYrz8/kjl0/XQNNkoh1Ji6NCH7EBFzv
B5DHIJSFqsJqRaO4+APPXirpuXF8vkrWgH+S0mQXYHL9Xl0CQQDIdAjUVXxHAMQp
bbf67NsiX6MVuvTTrUQq2b6Tf8KER6N1Fv5Mcr/ix6EikaL3E36h/iEL0t+TH2FA
OM1Z2Zt3AkEA/RjhuhGARoDkddRI6jhYgMfWphg319Pw1xqXMKufXS3snexBrNzS
POvlPSotreaCGBTV/+xp8a3n+OfTmikKQwJANxw/uTDvhA3f4Iv7ww8PiDnG+ph1
6yR901IeJStA7WFMvUpfC+GYg97inEByD3/alurpZvjI4wgDksaLHqLHLQJBAIEL
mbPsTnIkL9ggF9lMR1vKCJiBSp/B0U9roGDRcJzq2HUgy8+ee5dSU3yfL9E18Wjj
3sTxPodaOyd+1DYK7M8CQQCuMvbJC6Qm3/MkVX1Mo7gLqjqYHjqxcBDWzcpfk34T
Qs+WOs0V8wDRh3lylhdzE0waRofiDCnAhr7TzZc4rW/6
-----END RSA PRIVATE KEY-----`

	block, _ := pem.Decode([]byte(pemString))
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Println(err)
	}

	log.Println(key.N)

	client := bweethlib.NewBweeth()
	notifCh, err := client.Start(key, "127.0.0.1:6969", 3)
	if err != nil {
		log.Println(err)
		return
	}

	client.Post("hello world")

	for i := 0; i < 3; i++ {
		result := <-notifCh
		log.Println("NOTIFYCH RESULT:", result)
	}
}
