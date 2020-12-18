package main

import (
    "fmt"
	"strings"
	"context"

	"github.com/psanford/wormhole-william/wormhole"
)

func SendText(message string, reject func(error)) string {
	fmt.Println("SendText")
	c := newClient()

	var msg string
	msg = strings.TrimSpace(message)

	ctx := context.Background()
	codeFlag := ""
  	code, status, err := c.SendText(ctx, msg, wormhole.WithCode(codeFlag))
	fmt.Println("msg sent")
	if err != nil {
        fmt.Println("rejecting after c.SendText")
	    reject(err)
	    return ""
	}

    go func() {
        fmt.Println("reading from status")
        s := <-status

        if s.Error != nil {
            fmt.Println("rejecting with s.Error")
            reject(err)
        }
    }()

    return code
}

func newClient() wormhole.Client {
	c := wormhole.Client{
		RendezvousURL:             "", //relayURL,
		PassPhraseComponentLength: 2, //codeLen,
	}

	return c
}
