.PHONY: build install clean test

BINARY := agentmail
PREFIX ?= /usr/local
BINDIR := $(PREFIX)/bin

build:
	CGO_ENABLED=1 go build --tags "fts5" -o $(BINARY) .

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 $(BINARY) $(DESTDIR)$(BINDIR)/$(BINARY)

clean:
	rm -f $(BINARY)

test:
	go test ./...
