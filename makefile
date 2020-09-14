.PHONY: all fmt

all : dist/imap-print

dist/imap-print:
	@rm -rf dist
	@go build -o dist/imap-print *.go

fmt:
	@golint main.go
	@go vet main.go
	@gofmt -l -s -w main.go
