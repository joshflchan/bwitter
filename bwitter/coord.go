package bwitter

import (
	"fmt"
	"net/rpc"
)

type Coord struct {
}

func (c *Coord) Start() {
	err := rpc.Register(c)
	if err != nil {
		fmt.Println("failed to register")
	}
}

// TODO: do we only want to return x number of servers?
func (c *Coord) GetPeers() {

}
