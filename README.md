# Go Outbox

A simple, practical implementation of the **Outbox Pattern** in Go, featuring PostgreSQL, NATS, and support for
distributed relays.

---

## ✅ Features

- Distributed safety using PostgreSQL advisory locks(Leadership election can be exchanged by more advanced mechanisms)
- Occasional consistency which allows aggregate-specific events be retrieved in sequence even in case of failures.
- At least once delivery of events(a threshold of max_attempts is used to limit the number of retries)
- Configuration via file and environment variables, which enables cross-platform compatibility
- Structured logging
- Clean and testable architecture
- Extendable for future challenges

---

## 🏁 How to use

### Set up the sample application using Docker Compose

#### Prerequisites

- Docker installed
- Docker Compose installed
- Curl Command

In order to run a sample distributed setup which includes database, nats server and sample servers and consumers, use the following command:
```shell
make up
```

This command launches the following services in the background:
- A PostgreSQL database
- A NATS server
- Two sample servers (which are the producers of the events)
- One sample consumer (which is the consumer of the events on `users` topic)

At the meanwhile, in another terminal, you can listen on the logs to track the events and messages being sent/received to/by the NATS server
```shell
make sample-logs
```

Once the setup is up and running, you can then trigger some events using the following curl commands to create/verify users.

```shell
# Create a user through sample server 1
curl --location '127.0.0.1:8081/users' \
--header 'Content-Type: application/json' \
--data-raw '{
    "name":"john.doe",
    "email": "sample-user@gmail.de"
}
'
```

```shell
# Verify a user through sample server 2
curl --location --request PATCH '127.0.0.1:8082/users/1/verify'
```

Now you should be able to see track of changes in the logs.

You can also stop the containers using the following command in order to check if the other server keeps the leadership and sends the events.
if the sample-server-1 is the leader then stop it by:

```shell
docker-compose stop sample-server-1
```

And do not forget to cleanup the docker-compose artifacts at the end:
```shell
make down
```

### As a standalone Relay application

In this mode, the application will run as a standalone process and will start polling the outbox table for new messages
and sends them to the configured nats broker.
Using the leader election, it will ensure that only one instance of the application is actively polling the outbox table.

To use it as a standalone application, you can use the provided make command to build the application and run it:

```shell
make docker-build
````

Then it will build a Docker image called `outbox-relay:latest` which you can run using the following command:

```shell
docker run --rm -e OUTBOX_DATABASE_DSN='postgres://admin:admin123456@localhost:5432/outbox?sslmode=disable' \
  -e OUTBOX_NATS_URL=nats://localhost:4222 \
  -e CONFIG_PATH=/app/config/relay.toml \
  --network=host outbox-relay:latest
```

### As a bounded library inside the server instances

The go-outbox library can be embedded directly into your Go applications (like your server instances) to ensure reliable
message publishing as part of your local database transactions. This setup is ideal for distributed systems where each
service emits domain events when making state changes.

For a full application integration you can refer to the usage in the [sample-server app](cmd/sample-server/main.go).

1) Initialize the outbox storage:
   Inside your application initialization (e.g., App.Init()), initialize the outbox table and the storage driver:

```go
	// Init the db connection
    db, err := sql.Open("postgres", appCfg.DatabaseDSN)
    if err != nil {
      logger.Error("failed to connect to database", slog.Any("error", err))
      os.Exit(1)
    }
    
	// Initialize components
    storage := outbox.NewSQLStorage(db)
    if err = storage.InitOutboxTable(ctx); err != nil {
      logger.Error("failed to initialize outbox table", slog.Any("error", err))
      os.Exit(1)
    }
```

2) Insert messages as part of your DB transaction:
```go
  tx, err := db.BeginTx(ctx, nil)
  // ... insert/update your business data
  
  event := outbox.StorageRecord{
      EventType:     "OrderCreated",
      AggregateType: "Order",
      AggregateID:   "123", // can be stringified int or UUID
      Data:          []byte(`{"id":123,"total":1200}`),
      Topic:         "orders",
  }
  
  err = outboxStorage.InsertMessage(ctx, tx, event)
  err = tx.Commit()
```

3) Start/Stop the relay process:
   You can start the relay process in a separate goroutine or as a separate service. The relay will poll the outbox
   table and publish messages to the configured NATS broker.

```go
    // Initialise the relay dependencies.
    relay := outbox.NewRelay(storage, publisher, elector, appCfg.Relay, logger)
  
    go relay.Start(ctx) // non-blocking: runs in the background
	
	// Stop the relay process when your application is shutting down.
    relay.Shutdown(ctx) // optional: manual shutdown call if needed
```

#### How to run tests

```shell
make test
```

### Configuration

The application uses a configuration file to set up the relay app.
The default configuration file is `config/relay.toml`, but you can create your own by copying `config/relay.toml` and
modifying it as needed.
Then you can set the `CONFIG_PATH` environment variable to point to your custom configuration file.

#### Sample TOML Config

```toml
# config/relay.toml

database_dsn = "postgres://admin:admin123456@localhost:5432/outbox?sslmode=disable"
nats_url = "nats://localhost:4222"
advisory_lock = 42

[relay]
poll_interval = "3000ms"
batch_size = 100
max_attempts = 3

logging_level = "debug"
logging_format = "text"
```

#### Environment Variables

The application supports the following environment variables as the overrides to the config file:

| **Variable Name**       | **Type**      | **Default/Template**                                    | **Description**                       |
|-------------------------|---------------|---------------------------------------------------------|---------------------------------------|
| `OUTBOX_DATABASE_DSN`   | ***string***  | `postgres://user:pass@host:5432/outbox?sslmode=disable` | Postgres connection string            |
| `OUTBOX_NATS_URL`       | ***string***  | "nats://localhost:4222"                                 | NATS server URL                       |
| `OUTBOX_ADVISORY_LOCK`  | ***integer*** | 42                                                      | Advisory lock ID for the outbox table |
| `OUTBOX_POLL_INTERVAL`  | ***string***  | 1000ms                                                  | Polling interval for the outbox table |
| `OUTBOX_BATCH_SIZE`     | ***integer*** | 100                                                     | Batch size for processing messages    |
| `OUTBOX_MAX_ATTEMPTS`   | ***integer*** | 3                                                       | Maximum number of retries             |
| `OUTBOX_LOGGING_LEVEL`  | ***string***  | "info"                                                  | Log level (debug, info, warn, error)  |
| `OUTBOX_LOGGING_FORMAT` | ***string***  | "text"                                                  | Log format (json, text)               |
