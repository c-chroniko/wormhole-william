package main

import (
	"context"
	"io/ioutil"
	"log"
// 	"syscall/js"

	"github.com/psanford/wormhole-william/wormhole"
)

// Code -> Text message
func RecvText(code string) string {
	var c = newClient()
	var ctx = context.Background()

	// todo: verifier support
	msg, err := c.Receive(ctx, code)
	if err != nil {
		log.Fatal(err)
	}
	switch msg.Type {
	case wormhole.TransferText:
		body, err := ioutil.ReadAll(msg)
		if err != nil {
			log.Fatal(err)
		}
		return string(body)
	default:
		return "unsupported transfer type"
	}
}
