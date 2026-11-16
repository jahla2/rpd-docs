.PHONY: up down logs infra-up infra-down test go-test py-test cpp-build e2e

up:
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f --tail=200

infra-up:
	docker compose up -d postgres nats

infra-down:
	docker compose stop postgres nats

go-test:
	go test ./apps/api/... ./apps/orchestrator/...

py-test:
	python -m pytest apps/ai-engine/tests

cpp-build:
	cmake -S apps/native-engine -B build/native-engine
	cmake --build build/native-engine --config Release

e2e:
	python tests/end-to-end/e2e_smoke.py

test: go-test py-test
