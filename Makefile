.PHONY: infra-up infra-down test go-test py-test cpp-build

infra-up:
	docker compose up -d postgres nats

infra-down:
	docker compose down

go-test:
	go test ./apps/api/... ./apps/orchestrator/...

py-test:
	python -m pytest apps/ai-engine/tests

cpp-build:
	cmake -S apps/native-engine -B build/native-engine
	cmake --build build/native-engine --config Release

test: go-test py-test
