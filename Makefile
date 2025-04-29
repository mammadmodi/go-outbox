# Variables
APP_NAME=outbox-relay
DOCKER_COMPOSE_FILE=docker-compose.yml

# Build the Go application
build:
	go build -o $(APP_NAME) cmd/outbox-relay/main.go

# Run tests
test:
	go test ./... -v

# Run golangci-lint
lint:
	golangci-lint run

# Build the Docker image
docker-build: build
	docker build -t $(APP_NAME):latest -f Dockerfile-relay .

# Start Docker Compose
up:
	docker-compose -f $(DOCKER_COMPOSE_FILE) up -d

# Stop Docker Compose
down:
	docker-compose -f $(DOCKER_COMPOSE_FILE) down