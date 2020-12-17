package main

import (
	"fmt"
	"strings"
	"log"
	"context"
// 	"syscall/js"

	"github.com/psanford/wormhole-william/wormhole"
)

func SendText(message string) string {
	c := newClient()

	var msg string
	msg = strings.TrimSpace(message)
	fmt.Printf("sendText message: %s\n", msg)

	ctx := context.Background()
	codeFlag := ""
  	code, status, err := c.SendText(ctx, msg, wormhole.WithCode(codeFlag))
//   	code, _, err := c.SendText(ctx, msg, wormhole.WithCode(codeFlag))
	if err != nil {
		log.Fatal(err)
	}

    go func() {
        s := <-status

        if s.Error != nil {
            log.Fatalf("Send error: %s", s.Error)
        } else if s.OK {
            fmt.Println("text message sent")
        } else {
            log.Fatalf("Hmm not ok but also not error")
        }
    }()
    fmt.Printf("got a code: %v\n", code)

    return code;
}

func newClient() wormhole.Client {
	c := wormhole.Client{
		RendezvousURL:             "", //relayURL,
		PassPhraseComponentLength: 2, //codeLen,
	}

	return c
}
