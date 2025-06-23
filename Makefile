# Makefile for LagRadar


BINARY_NAME=LagRadar
GO=go
GOFLAGS=-v


.PHONY: all
all: build


.PHONY: build
build:
	$(GO) build $(GOFLAGS) -o $(BINARY_NAME) main.go


.PHONY: run
run:
	$(GO) run main.go


.PHONY: run-all
run-all:
	$(GO) run main.go


.PHONY: run-group
run-group:
	$(GO) run main.go -group $(GROUP)


.PHONY: clean
clean:
	rm -f $(BINARY_NAME)


.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make build      - Build the binary"
	@echo "  make run        - Run and show ALL consumer groups"
	@echo "  make run-all    - Run and show ALL consumer groups (alias)"
	@echo "  make run-group GROUP=<n> - Run for specific consumer group"
	@echo "  make clean      - Remove binary"
