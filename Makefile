.PHONY: generate lint test build docker up down smoke-test

generate:                    ## Generate sqlc + protobuf code
	sqlc generate
	buf generate

lint:                        ## Code check
	golangci-lint run ./...
	buf lint

test:                        ## Run tests
	go test -race -count=1 ./...

build:                       ## Build all binaries
	CGO_ENABLED=0 go build -o bin/wvs-api ./cmd/wvs-api
	CGO_ENABLED=0 go build -o bin/wvs-worker ./cmd/wvs-worker
	CGO_ENABLED=0 go build -o bin/wvs-executor ./cmd/wvs-executor
	CGO_ENABLED=0 go build -o bin/wvsctl ./cmd/wvsctl

docker:                      ## Build Docker images
	docker build -t yourorg/wvs-api -f Dockerfile.api .
	docker build -t yourorg/wvs-worker -f Dockerfile.worker .
	docker build -t yourorg/wvs-executor -f Dockerfile.executor .

up:                          ## Start demo environment
	docker-compose up -d

down:                        ## Stop demo environment
	docker-compose down -v

smoke-test:                  ## End-to-end smoke test
	docker-compose up -d
	bash scripts/smoke-test.sh
