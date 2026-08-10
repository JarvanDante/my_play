APP := my_play
BIN := bin
MYDOCKER := /Users/wangdante/D/mydocker

.PHONY: build dev tidy clean docker-up docker-down docker-logs docker-rebuild

build:
	go build -o $(BIN)/playgw .

dev:
	gf run main.go

tidy:
	go mod tidy

clean:
	rm -rf $(BIN)

# ---- Docker：走 mydocker 主编排 ----
docker-up:
	cd $(MYDOCKER) && docker compose up -d --build my_play
	@echo "探活: curl -sS http://127.0.0.1:8006/healthz"

docker-down:
	cd $(MYDOCKER) && docker compose stop my_play

docker-logs:
	docker logs -f my_play

docker-rebuild:
	cd $(MYDOCKER) && docker compose up -d --build --force-recreate my_play
