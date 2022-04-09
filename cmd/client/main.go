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
MIICXQIBAAKBgQC/njt5NpRDPeYh/UZXfK+kFrUPKQ766mrORhxdJRyyus0c9f4m
95gp1BCIUS+ypAKIKj90Jsb4NoI6AOR3FFuBOywpZCXTBZlRKR7VyYL/4XXPCKI3
ujBpQ26dEh71bS3eb2KGOBJ7J7ONpVLYf8Nk67VUL6SGThFjcgINu1AhaQIDAQAB
AoGAUaUGCjurKItzRwA3vIcv/2Z9dxwzec3v/Dv7UeTCOZVGWBSoWcodV5U4Bh0x
iZhAS+xUZRUwqgnvqu8+aU6PPeBOZON2ZH/EU6aRBcTj0dhpyP15nLY4kre49myV
3/ahFl9TjiqD7g7zguNzr7Ba01tXw8aOEqi4Cx4pUmSoHAECQQDce1MwDRU38PCS
f4KDxZ3C8IqKT47sNTc8MOJyx9sv5GEnPxIviF4Ay+7DBnTHYI6tyLpFuiKy6qx7
kK8q4ncJAkEA3nyR1PEsfkHsiPAbvCwkJKXkT/JqLBEwfELX3yUdbNb073NPFjLU
66FON6URW12PFJFDDj1T4qKEclA2uKuPYQJBAJFAMcqbI9ppwaNRm4MCIm+1lh+O
UCLu4AeoUNa7MXw4oYSAeZOU7BDsSMx0qWRcCUMV1RlwicGC9sSkybGf5jkCQBx6
q5wrZvueaq24toQmzlWWmpwVNrv/U0qEr+dTc+nLtjy0cOoxhYnH8yAyU/9zAW6r
jX7UINnA3d1YITkQVOECQQCXd/4fwNenP4NZ3G9PkpYz13e2xNhegc3OEHKleLx/
wlYUzHOsg6HZTQbZuTvl5RQZlH8yEeQEVIFDiMWJl/4U
-----END RSA PRIVATE KEY-----`

	block, _ := pem.Decode([]byte(pemString))
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Println(err)
	}

	log.Println(key.N)

	client := bweethlib.NewBweeth()
	notifCh, err := client.Start(key, "127.0.0.1:6505", 3)
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
