package main

import (
	"fmt"
	"strings"
	"log"
	"context"
	"syscall/js"

	"github.com/psanford/wormhole-william/wormhole"
)

func SendText(message string) {
	c := newClient()

	var msg string
	msg = strings.TrimSpace(message)

	ctx := context.Background()
	codeFlag := ""
  	code, status, err := c.SendText(ctx, msg, wormhole.WithCode(codeFlag))
	if err != nil {
		log.Fatal(err)
	}

	printInstructions(code)

	s := <-status

	if s.Error != nil {
		log.Fatalf("Send error: %s", s.Error)
	} else if s.OK {
		fmt.Println("text message sent")
	} else {
		log.Fatalf("Hmm not ok but also not error")
	}
}

func newClient() wormhole.Client {
	c := wormhole.Client{
		RendezvousURL:             "", //relayURL,
		PassPhraseComponentLength: 2, //codeLen,
	}

	return c
}

func printInstructions(code string) {
	mwCmd := "wormhole receive"

	t1 := fmt.Sprintf("On the other computer, please run: %s\n", mwCmd)
	t2 := fmt.Sprintf("Wormhole code is: %s\n", code)

	jsDoc := js.Global().Get("document")
	if !jsDoc.Truthy() {
		return
	}

	outputArea1 := jsDoc.Call("createElement", "p")
	outputArea1.Set("innerHTML", t1)
	jsDoc.Get("body").Call("appendChild", outputArea1)

	outputArea2 := jsDoc.Call("createElement", "p")
	outputArea2.Set("innerHTML", t2)
	jsDoc.Get("body").Call("appendChild", outputArea2)
}
