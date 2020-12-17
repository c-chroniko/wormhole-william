package main

import (
	"strings"
// 	"log"
	"context"
// 	"syscall/js"

	"github.com/psanford/wormhole-william/wormhole"
)

func SendText(message string, reject func(error)) string {
	c := newClient()

	var msg string
	msg = strings.TrimSpace(message)

	ctx := context.Background()
	codeFlag := ""
//   	code, status, err := c.SendText(ctx, msg, wormhole.WithCode(codeFlag))
  	code, _, err := c.SendText(ctx, msg, wormhole.WithCode(codeFlag))
	if err != nil {
	    reject(err)
	    return ""
	}

//     go func() {
//         s := <-status
//
//         if s.Error != nil {
//             log.Fatalf("Send error: %s", s.Error)
//         } else if s.OK {
//             fmt.Println("text message sent")
//         } else {
//             log.Fatalf("Hmm not ok but also not error")
//         }
//     }()

    return code;
}

func newClient() wormhole.Client {
	c := wormhole.Client{
		RendezvousURL:             "", //relayURL,
		PassPhraseComponentLength: 2, //codeLen,
	}

	return c
}
