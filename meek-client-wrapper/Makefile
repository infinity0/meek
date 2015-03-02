DESTDIR =
PREFIX = /usr/local
BINDIR = $(PREFIX)/bin

GOBUILDFLAGS =

all: meek-client-wrapper

meek-client-wrapper: *.go
	go build $(GOBUILDFLAGS)

clean:
	rm -f meek-client-wrapper

fmt:
	go fmt

.PHONY: all install clean fmt
