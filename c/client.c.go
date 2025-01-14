// +build cgo

package main

import (
	"bytes"
	"context"
	"fmt"
	"unsafe"

	"github.com/psanford/wormhole-william/c/codes"
	"github.com/psanford/wormhole-william/wormhole"
	"io/ioutil"
)

// #include <stdlib.h>
// #include "client.h"
import "C"

func main() {

}

// TODO: refactor?
const (
	DEFAULT_APP_ID                      = "lothar.com/wormhole/text-or-file-xfer"
	DEFAULT_RENDEZVOUS_URL              = "ws://relay.magic-wormhole.io:4000/v1"
	DEFAULT_TRANSIT_RELAY_URL           = "tcp:transit.magic-wormhole.io:4001"
	DEFAULT_PASSPHRASE_COMPONENT_LENGTH = 2
)

type ClientsMap = map[uintptr]*wormhole.Client

var (
	ErrClientNotFound = fmt.Errorf("%s", "wormhole client not found")

	clientsMap ClientsMap
)

func init() {
	clientsMap = make(ClientsMap)
}

//export NewClient
func NewClient(appId *C.char, rendezvousUrl *C.char, transitRelayUrl *C.char, passPhraseComponentLength C.int) uintptr {
	client := &wormhole.Client{
		AppID:                     DEFAULT_APP_ID,
		RendezvousURL:             DEFAULT_RENDEZVOUS_URL,
		TransitRelayURL:           DEFAULT_TRANSIT_RELAY_URL,
		PassPhraseComponentLength: DEFAULT_PASSPHRASE_COMPONENT_LENGTH,
	}

	if appId != nil {
		client.AppID = C.GoString(appId)
	}

	if rendezvousUrl != nil {
		client.RendezvousURL = C.GoString(rendezvousUrl)
	}

	if transitRelayUrl != nil {
		client.TransitRelayURL = C.GoString(transitRelayUrl)
	}

	if passPhraseComponentLength == 0 {
		client.PassPhraseComponentLength = int(passPhraseComponentLength)
	}

	clientPtr := uintptr(unsafe.Pointer(client))
	clientsMap[clientPtr] = client

	return clientPtr
}

//export FreeClient
func FreeClient(clientPtr uintptr) int {
	if _, err := getClient(clientPtr); err != nil {
		return int(codes.ERR_NO_CLIENT)
	}

	delete(clientsMap, clientPtr)
	return int(codes.OK)
}

//export ClientSendText
func ClientSendText(ptrC unsafe.Pointer, clientPtr uintptr, msgC *C.char, codeOutC **C.char, cb C.callback) int {
	ctx := context.Background()
	client, err := getClient(clientPtr)
	if err != nil {
		return int(codes.ERR_NO_CLIENT)
	}

	// TODO: return code asynchronously (i.e. from a go routine).
	//	This call blocks on network I/O with the mailbox.
	code, status, err := client.SendText(ctx, C.GoString(msgC))
	if err != nil {
		return int(codes.ERR_SEND_TEXT)
	}
	*codeOutC = C.CString(code)

	go func() {
		resultC := (*C.result_t)(C.malloc(C.sizeof_result_t))
		*resultC = C.result_t{}
		s := <-status
		if s.Error != nil {
			resultC.err_code = C.int(codes.ERR_SEND_TEXT_RESULT)
			resultC.err_string = C.CString(s.Error.Error())
		} else if s.OK {
			resultC.err_code = C.int(codes.OK)
		} else {
			resultC.err_code = C.int(codes.ERR_UNKNOWN)
			resultC.err_string = C.CString(codes.ERR_UNKNOWN.String())
		}
		C.call_callback(ptrC, cb, resultC)
	}()

	return int(codes.OK)
}

//export ClientSendFile
func ClientSendFile(ptrC unsafe.Pointer, clientPtr uintptr, fileName *C.char, length C.int, fileBytes unsafe.Pointer, codeOutC **C.char, cb C.callback) int {
	client, err := getClient(clientPtr)
	if err != nil {
		return int(codes.ERR_NO_CLIENT)
	}
	ctx := context.Background()

	// TODO: is there a way to avoid copying?
	reader := bytes.NewReader(C.GoBytes(fileBytes, length))

	// TODO: return code asynchronously (i.e. from a go routine).
	//	This call blocks on network I/O with the mailbox.
	code, status, err := client.SendFile(ctx, C.GoString(fileName), reader, false)
	if err != nil {
		return int(codes.ERR_SEND_TEXT)
	}
	*codeOutC = C.CString(code)

	go func() {
		resultC := (*C.result_t)(C.malloc(C.sizeof_result_t))
		*resultC = C.result_t{}
		s := <-status
		if s.Error != nil {
			resultC.err_code = C.int(codes.ERR_SEND_FILE_RESULT)
			resultC.err_string = C.CString(s.Error.Error())
		} else if s.OK {
			resultC.err_code = C.int(codes.OK)
		} else {
			resultC.err_code = C.int(codes.ERR_UNKNOWN)
			resultC.err_string = C.CString("Unknown error")
		}
		C.call_callback(ptrC, cb, resultC)
	}()

	return int(codes.OK)
}

//export ClientRecvText
func ClientRecvText(ptrC unsafe.Pointer, clientPtr uintptr, codeC *C.char, cb C.callback) int {
	client, err := getClient(clientPtr)
	if err != nil {
		return int(codes.ERR_NO_CLIENT)
	}
	ctx := context.Background()

	go func() {
		resultC := (*C.result_t)(C.malloc(C.sizeof_result_t))
		*resultC = C.result_t{}
		msg, err := client.Receive(ctx, C.GoString(codeC), false)
		if err != nil {
			resultC.err_code = C.int(codes.ERR_RECV_TEXT)
			resultC.err_string = C.CString(err.Error())
			C.call_callback(ptrC, cb, resultC)
			return
		}

		data, err := ioutil.ReadAll(msg)
		if err != nil {
			resultC.err_code = C.int(codes.ERR_RECV_TEXT_DATA)
			resultC.err_string = C.CString(err.Error())
			C.call_callback(ptrC, cb, resultC)
			return
		}

		resultC.received_text = C.CString(string(data))
		resultC.err_code = C.int(codes.OK)
		C.call_callback(ptrC, cb, resultC)
	}()

	return int(codes.OK)
}

//export ClientRecvFile
func ClientRecvFile(ptrC unsafe.Pointer, clientPtr uintptr, codeC *C.char, cb C.callback) int {
	client, err := getClient(clientPtr)
	if err != nil {
		return int(codes.ERR_NO_CLIENT)
	}
	ctx := context.Background()

	go func() {
		resultC := (*C.result_t)(C.malloc(C.sizeof_result_t))
		*resultC = C.result_t{}
		msg, err := client.Receive(ctx, C.GoString(codeC), false)

		if err != nil {
			resultC.err_code = C.int(codes.ERR_RECV_FILE)
			resultC.err_string = C.CString(err.Error())
			C.call_callback(ptrC, cb, resultC)
			return
		}

		data, err := ioutil.ReadAll(msg)
		if err != nil {
			resultC.err_code = C.int(codes.ERR_RECV_TEXT_DATA)
			resultC.err_string = C.CString(err.Error())
			C.call_callback(ptrC, cb, resultC)
			return
		}

		bytesC := C.CBytes(data)
		fileC := (*C.file_t)(C.malloc(C.sizeof_file_t))
		*fileC = C.file_t{
			length: C.int(len(data)),
			data:   (*C.uint8_t)(bytesC),
		}
		resultC.err_code = C.int(codes.OK)
		resultC.file = fileC
		resultC.file.file_name = C.CString(msg.Name)
		C.call_callback(ptrC, cb, resultC)
	}()

	return int(codes.OK)
}

// TODO: refactor w/ wasm package?
func getClient(clientPtr uintptr) (*wormhole.Client, error) {
	client, ok := clientsMap[clientPtr]
	if !ok {
		fmt.Printf("clientMap entry missing: %d\n", uintptr(clientPtr))
		fmt.Printf("clientMap entry missing: %d\n", clientPtr)
		return nil, ErrClientNotFound
	}

	return client, nil
}
