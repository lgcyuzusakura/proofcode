.PHONY: test test-go test-control test-frontend up down logs

test: test-go test-control test-frontend

test-go:
	docker compose -f compose.test.yml run --rm agent-test

test-control:
	docker compose -f compose.test.yml run --rm control-test

test-frontend:
	docker compose -f compose.test.yml run --rm frontend-test

up:
	docker compose up --build

down:
	docker compose down --remove-orphans

logs:
	docker compose logs -f --tail=200
