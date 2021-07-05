#!/bin/sh

go build -buildmode=c-shared -o libwormhole_william.so ../../c/client.c.go

# C program to exercise the exported library API.
gcc ww.c -I. -L. -lwormhole_william -o ww
