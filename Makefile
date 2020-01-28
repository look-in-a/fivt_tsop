PROJECT := mocker


GOPATH   := $(shell pwd)/vendor:$(shell pwd)
PATH 	 := $(shell pwd)/vendor/bin:$(shell pwd)/bin:$(PATH)

all:
	PATH="$(PATH)" GOPATH="$(GOPATH)" go build  src/mocker.go
