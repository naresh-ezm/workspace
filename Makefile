.PHONY: build build-ui build-api dev-ui dev-api

build: build-ui build-api

build-ui:
	cd web && npm run build

build-api:
	go build -o ./bin/app .

# Run the app locally — build UI first so embed has files to include
dev:
	$(MAKE) build-ui
	go run main.go

dev-ui:
	cd web && npm run dev
