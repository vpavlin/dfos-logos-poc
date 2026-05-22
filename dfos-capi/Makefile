.PHONY: build clean test

build: dfos.so

dfos.so: *.go go.mod go.sum
	go build -buildmode=c-shared -o dfos.so .

smoke_test: smoke_test.c dfos.so
	gcc -o smoke_test smoke_test.c ./dfos.so -Wl,-rpath,'$$ORIGIN'

test: smoke_test
	rm -rf /tmp/dfos-smoke-test
	LD_LIBRARY_PATH=. ./smoke_test

clean:
	rm -f dfos.so dfos.h smoke_test
