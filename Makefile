PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64

.PHONY: install dist server check

# Build agentsync for this machine.
install:
	go build -o $(HOME)/.local/bin/agentsync ./cmd/agentsync

# Cross-compile the client binaries that the server offers at /bin/.
dist:
	@for platform in $(PLATFORMS); do \
		echo "dist/agentsync-$$platform"; \
		GOOS=$${platform%-*} GOARCH=$${platform#*-} CGO_ENABLED=0 \
			go build -trimpath -ldflags "-s -w" -o dist/agentsync-$$platform ./cmd/agentsync || exit 1; \
	done

# Set this machine up as the server: run the server for this checkout as a
# systemd user service, then link this checkout into the local harnesses.
server: install dist
	@test -f AGENTS.md || { echo "No AGENTS.md here. Add your AGENTS.md and skills/ to $(CURDIR) first."; exit 1; }
	mkdir -p $(HOME)/.config/systemd/user
	sed 's|@DIR@|$(CURDIR)|' deploy/agentsync-server.service > $(HOME)/.config/systemd/user/agentsync-server.service
	systemctl --user daemon-reload
	systemctl --user enable agentsync-server
	systemctl --user restart agentsync-server
	$(HOME)/.local/bin/agentsync init
	$(HOME)/.local/bin/agentsync link --dir $(CURDIR)
	@echo
	@echo "Server is running. Set up each client with:"
	@echo "  curl -fsSL http://$$(hostname):8377/install.sh | sh"

check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
