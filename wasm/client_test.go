// +build js,wasm

package wasm

import (
	"context"
	"fmt"
	"syscall/js"
	"testing"
)

var (
	wasmCtx context.Context
	wasmCancel context.CancelFunc
)

func TestMain(m *testing.M) {
	wasmCtx, wasmCancel = context.WithCancel(context.Background())
	wasmCtx.Done()

	m.Run()
}

func TestClient_SendText(t *testing.T) {
	jsClientPtr := NewClientWrapper()
	msg := "testing 123"
	sendPromise := Client_SendText(js.Undefined(), []js.Value{jsClientPtr, js.ValueOf(msg)})
	// TODO: make assertions
	js.ValueOf(sendPromise).Call("then", js.FuncOf(func(_ js.Value, args[]js.Value) interface{} {
		fmt.Println("client_test.go:17| TestClient_SendText!")
		fmt.Printf("client_test.go:18| code: %s\n", args[0].String())
		wasmCancel()
		return nil
	}))

	<- wasmCtx.Done()
}

func NewClientWrapper() js.Value {
	return js.FuncOf(NewClient).Invoke(js.Undefined(), nil)
}
