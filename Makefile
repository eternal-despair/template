-include .env
export

.PHONY: generate
generate:
	go tool oapi-codegen \
		-generate types,chi-server \
		-package api \
		-include-operation-ids createTrip,getTrip,finishTrip,health,ready \
		-o internal/generated/api.gen.go \
		contracts/openapi/trip-service.openapi.yaml

.PHONY: migrate
migrate:
	go tool goose -dir migrations postgres "$$(DATABASE_URL)" up

.PHONY: migrate-down
migrate-down:
	go tool goose -dir migrations postgres "$$(DATABASE_URL)" down

.PHONY: test
test:
	go test -race ./...

.PHONY: run
run:
	go run ./cmd/trip-service
