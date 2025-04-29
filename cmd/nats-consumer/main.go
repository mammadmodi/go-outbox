package main

import (
	"log"
	"os"

	"github.com/nats-io/nats.go"
)

func main() {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}

	topic := os.Getenv("NATS_TOPIC")
	if topic == "" {
		topic = "users"
	}

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Drain()

	_, err = nc.Subscribe(topic, func(m *nats.Msg) {
		log.Printf("Received a message on topic [%s]: %s\n", m.Subject, string(m.Data))
	})
	if err != nil {
		log.Fatalf("Failed to subscribe to topic: %v", err)
	}

	log.Printf("Listening for messages on topic [%s]...", topic)
	select {} // Block forever, just for demonstration purposes
}
