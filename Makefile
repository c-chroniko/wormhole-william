all: wasm

wasm: wasm_server/wormhole.wasm wasm_server/wasmsrv

wasm_server/wormhole.wasm: wasm_server/wasm_exec.js
	# build wormhole-william wasm code
	GOOS=js GOARCH=wasm go build -o wasm_server/wormhole.wasm

wasm_server/wasmsrv: wasm_server/wasmsrv.go
	# build the server
	go build -o wasm_server/wasmsrv wasm_server/wasmsrv.go

wasm_server/wasm_exec.js:
	cp "$(shell go env GOROOT)/misc/wasm/wasm_exec.js" wasm_server/

clean:
	-go clean
	-rm wasm_server/wormhole.wasm
	-rm wasm_server/wasmsrv

