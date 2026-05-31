BINARY   := edcode
GO       := go
BINDIR   ?= $(HOME)/.local/bin
GOFLAGS  := -ldflags="-s -w"

.PHONY: all build install setup configure reset uninstall clean reinstall

all: build

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) ./cmd/edcode/

install: build
	@mkdir -p $(BINDIR)
	@cp $(BINARY) $(BINDIR)/$(BINARY)
	@echo ""
	@echo "✓ edcode installed to $(BINDIR)/$(BINARY)"
	@echo "  Make sure $(BINDIR) is in your PATH."
	@echo "  Or run:  sudo make install-system"

install-system: build
	sudo install -d /usr/local/bin
	sudo install -m 755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo ""
	@echo "✓ edcode installed to /usr/local/bin/$(BINARY)"

setup: build configure
	@echo ""
	@echo "✓ edcode is ready. Run 'edcode --help' to get started."

configure:
	@echo ""
	@echo "Running interactive provider setup..."
	@$(CURDIR)/$(BINARY) --configure

reset:
	@rm -f $(CURDIR)/edcode.yaml
	@rm -rf $(HOME)/.edcode
	@echo "✓ All edcode configuration and data removed."

uninstall:
	@rm -f $(BINARY) $(BINDIR)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "✓ edcode uninstalled."

clean:
	@rm -f $(BINARY)
	@echo "✓ Build artifacts cleaned."

reinstall: uninstall install setup
	@echo "✓ Reinstalled."
