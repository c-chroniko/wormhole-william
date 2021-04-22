// +build js,wasm

package wasm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"syscall/js"
	"unsafe"

	"github.com/psanford/wormhole-william/wormhole"
)

type ClientMap = map[uintptr]*wormhole.Client

// TODO: automate use of `-ld -X` with env vars
const DEFAULT_APP_ID = "myFileTransfer"
const DEFAULT_RENDEZVOUS_URL = "ws://localhost:4000/v1"
const DEFAULT_TRANSIT_RELAY_URL = "ws://localhost:4002"
const DEFAULT_PASSPHRASE_COMPONENT_LENGTH = 2

var (
	ErrClientNotFound = fmt.Errorf("%s", "wormhole client not found")

	clientMap ClientMap
)

func init() {
	clientMap = make(ClientMap)
}

func NewClient(_ js.Value, args []js.Value) interface{} {
	var (
		config js.Value
		object = js.Global().Get("Object")
	)
	if len(args) > 0 && args[0].InstanceOf(object) {
		config = args[0]
	} else {
		config = object.New()
	}

	// read from config
	appID := config.Get("appID")
	rendezvousURL := config.Get("rendezvousURL")
	transitRelayURL := config.Get("transitRelayURL")
	passPhraseComponentLength := config.Get("passPhraseComponentLength")

	//overwrite config with defaults where falsy
	//TODO: use constants for property names?
	if !appID.Truthy() {
		config.Set("appID", DEFAULT_APP_ID)
	}
	if !rendezvousURL.Truthy() {
		config.Set("rendezvousURL", DEFAULT_RENDEZVOUS_URL)
	}
	if !transitRelayURL.Truthy() {
		config.Set("transitRelayURL", DEFAULT_TRANSIT_RELAY_URL)
	}
	if !passPhraseComponentLength.Truthy() {
		config.Set("passPhraseComponentLength", DEFAULT_PASSPHRASE_COMPONENT_LENGTH)
	}

	// read config with defaults merged
	// TODO: need this?
	appID = config.Get("appID")
	rendezvousURL = config.Get("rendezvousURL")
	transitRelayURL = config.Get("transitRelayURL")
	passPhraseComponentLength = config.Get("passPhraseComponentLength")

	client := &wormhole.Client{
		AppID:                     appID.String(),
		RendezvousURL:             rendezvousURL.String(),
		TransitRelayURL:           transitRelayURL.String(),
		PassPhraseComponentLength: passPhraseComponentLength.Int(),
	}
	clientPtr := uintptr(unsafe.Pointer(client))
	clientMap[clientPtr] = client

	return clientPtr
}

func Client_SendText(_ js.Value, args []js.Value) interface{} {
	ctx := context.Background()

	return NewPromise(func(resolve ResolveFn, reject RejectFn) {
		if len(args) != 2 {
			reject(fmt.Errorf("invalid number of arguments: %d. expected: %d", len(args), 2))
			return
		}

		clientPtr := uintptr(args[0].Int())
		msg := args[1].String()
		err, client := getClient(clientPtr)
		if err != nil {
			reject(err)
			return
		}

		code, _, err := client.SendText(ctx, msg)
		if err != nil {
			reject(err)
			return
		}
		resolve(code)
	})
}

func Client_SendFile(_ js.Value, args []js.Value) interface{} {
	ctx, cancel := context.WithCancel(context.Background())

	return NewPromise(func(resolve ResolveFn, reject RejectFn) {
		if len(args) != 3 && len(args) != 4 {
			reject(fmt.Errorf("invalid number of arguments: %d. expected: %s", len(args), "3 or 4"))
			return
		}

		clientPtr := uintptr(args[0].Int())
		fileName := args[1].String()

		uint8Array := args[2]
		size := uint8Array.Get("byteLength").Int()
		fileData := make([]byte, size)
		js.CopyBytesToGo(fileData, uint8Array)
		fileReader := bytes.NewReader(fileData)

		err, client := getClient(clientPtr)
		if err != nil {
			reject(err)
			return
		}

		var code string
		var resultChan chan SendResult
		if len(args) == 4 && !args[3].IsUndefined() {
			withProgress := wormhole.WithProgress(func(sentBytes int64, totalBytes int64) {
				args[3].Invoke(sentBytes, totalBytes)
			})
			code, resultChan, err = client.SendFile(ctx, fileName, fileReader, withProgress)
		} else {
			code, resultChan, err = client.SendFile(ctx, fileName, fileReader)
		}
		if err != nil {
			reject(err)
			return
		}

		returnObj := js.Global().Get("Object").New()
		returnObj.Set("code", code)
		returnObj.Set("cancel", js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
			cancel()
			return nil
		}))
		returnObj.Set("result", NewPromise(
			func(resolve ResolveFn, reject RejectFn) {
				select {
				case result := <-resultChan:
					switch {
					case result.Error != nil:
						reject(result.Error)
					case result.OK == true:
						resolve(nil)
					default:
						reject(errors.New("unknown send result"))
					}
				case <-ctx.Done():
					if err := ctx.Err(); err == nil {
						resolve(nil)
					} else {
						reject(err)
					}
				}
			}),
		)
		resolve(returnObj)
	})
}

func Client_RecvText(_ js.Value, args []js.Value) interface{} {
	ctx := context.Background()

	return NewPromise(func(resolve ResolveFn, reject RejectFn) {
		if len(args) != 2 {
			reject(fmt.Errorf("invalid number of arguments: %d. expected: %d", len(args), 2))
			return
		}

		clientPtr := uintptr(args[0].Int())
		code := args[1].String()
		err, client := getClient(clientPtr)
		if err != nil {
			reject(err)
			return
		}

		msg, err := client.Receive(ctx, code)
		if err != nil {
			reject(err)
			return
		}

		msgBytes, err := ioutil.ReadAll(msg)
		if err != nil {
			reject(err)
			return
		}
		resolve(string(msgBytes))
	})
}

func Client_RecvFile(_ js.Value, args []js.Value) interface{} {
	ctx, cancel := context.WithCancel(context.Background())

	return NewPromise(func(resolve ResolveFn, reject RejectFn) {
		// TODO: improve
		go func() {
			<-ctx.Done()
			if err := ctx.Err(); err != nil {
				reject(err)
			}
		}()

		if len(args) != 2 && len(args) != 3 {
			reject(fmt.Errorf("invalid number of arguments: %d. expected: %d or %d", len(args), 2, 3))
			return
		}

		clientPtr := uintptr(args[0].Int())
		code := args[1].String()
		err, client := getClient(clientPtr)
		if err != nil {
			reject(err)
			return
		}

		var msg *wormhole.IncomingMessage
		if len(args) == 3 && !args[2].IsUndefined() {
			withProgress := wormhole.WithProgress(func(sentBytes int64, totalBytes int64) {
				args[2].Invoke(sentBytes, totalBytes)
			})
			msg, err = client.Receive(ctx, code, withProgress)
		} else {
			fmt.Println("client.go:217| no")
			msg, err = client.Receive(ctx, code)
		}
		if err != nil {
			reject(err)
			return
		}

		readerObj := NewFileStreamReader(ctx, msg)
		readerObj.Set("cancel", js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
			cancel()
			return nil
		}))
		resolve(readerObj)
	})
}

func NewFileStreamReader(ctx context.Context, msg *wormhole.IncomingMessage) js.Value {
	// TODO: parameterize
	bufSize := 1024 * 4 // 4KiB

	total := 0
	readFunc := func(_ js.Value, args []js.Value) interface{} {
		buf := make([]byte, bufSize)
		return NewPromise(func(resolve ResolveFn, reject RejectFn) {
			// TODO: improve
			go func() {
				<-ctx.Done()
				if err := ctx.Err(); err != nil {
					reject(err)
				}
			}()

			if len(args) != 1 {
				reject(fmt.Errorf("invalid number of arguments: %d. expected: %d", len(args), 1))
			}

			jsBuf := args[0]
			_resolve := func(n int, done bool) {
				js.CopyBytesToJS(jsBuf, buf[:n])
				resolve(js.Global().Get("Array").New(n, done))
			}
			n, err := msg.Read(buf)
			total += n
			if err != nil {
				reject(err)
				return
			}
			if msg.ReadDone() {
				_resolve(n, true)
				return
			}
			_resolve(n, false)
		})
	}
	//TODO: refactor JS dependency injection
	// NB: this requires that streamsaver is available at `window.StreamSaver`
	readerObj := js.Global().Get("Object").New() //bufSize, js.FuncOf(readFunc))
	readerObj.Set("bufferSizeBytes", bufSize)
	readerObj.Set("read", js.FuncOf(readFunc))
	readerObj.Set("name", msg.Name)
	readerObj.Set("size", msg.UncompressedBytes64)
	return readerObj
}

func Client_free(_ js.Value, args []js.Value) interface{} {
	if len(args) != 1 {
		return fmt.Errorf("invalid number of arguments: %d. expected: %d", len(args), 2)
	}

	clientPtr := uintptr(args[0].Int())
	delete(clientMap, clientPtr)
	return js.Undefined()
}

func getClient(clientPtr uintptr) (error, *wormhole.Client) {
	client, ok := clientMap[clientPtr]
	if !ok {
		fmt.Println("clientMap entry missing")
		return ErrClientNotFound, nil
	}

	return nil, client
}
