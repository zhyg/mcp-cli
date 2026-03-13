BIN_DIR := $(CURDIR)/bin

.PHONY: all clean typescript golang rust

all: typescript golang rust

typescript:
	@mkdir -p $(BIN_DIR)/typescript
	cd typescript && bun build --compile --minify src/index.ts --outfile $(BIN_DIR)/typescript/mcp-cli

golang:
	@mkdir -p $(BIN_DIR)/golang
	cd golang && go build -o $(BIN_DIR)/golang/mcp-cli .

rust:
	@mkdir -p $(BIN_DIR)/rust
	cd rust && cargo build --release
	cp rust/target/release/mcp-cli $(BIN_DIR)/rust/mcp-cli

clean:
	rm -rf $(BIN_DIR)
