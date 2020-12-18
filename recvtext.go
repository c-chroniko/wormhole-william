package main

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/psanford/wormhole-william/wormhole"
)

// Code -> Text message
func RecvText(code string, reject func(error)) string {
    fmt.Println("RecvText")
	var c = newClient()
	var ctx = context.Background()

	// todo: verifier support
	msg, err := c.Receive(ctx, code)
	fmt.Println("msg received")
	if err != nil {
        fmt.Println("rejecting after c.Receive")
        reject(err)
        return ""
	}
	switch msg.Type {
	case wormhole.TransferText:
		body, err := ioutil.ReadAll(msg)
		if err != nil {
            fmt.Println("rejecting after ioutil.ReadAll")
            reject(err)
            return ""
		}

        return string(body)
	default:
        fmt.Println("rejecting unsupported transfer type")
		reject(fmt.Errorf("unsupported transfer type"))
		return ""
	}
}
