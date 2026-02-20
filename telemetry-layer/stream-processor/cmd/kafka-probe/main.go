package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IBM/sarama"
)

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func splitBrokers() []string {
	raw := getenv("KAFKA_BROKERS", "")
	if raw == "" {
		raw = getenv("KAFKA_BOOTSTRAP_SERVERS", "")
	}
	var brokers []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			brokers = append(brokers, v)
		}
	}
	return brokers
}

func buildConfig() (*sarama.Config, error) {
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_0_0_0
	cfg.ClientID = "telemetry-sarama-probe"
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true
	cfg.Producer.Partitioner = sarama.NewManualPartitioner
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Timeout = 10 * time.Second
	cfg.Consumer.Return.Errors = true
	cfg.Consumer.Offsets.Initial = sarama.OffsetNewest
	cfg.Net.DialTimeout = 10 * time.Second
	cfg.Net.ReadTimeout = 10 * time.Second
	cfg.Net.WriteTimeout = 10 * time.Second

	if strings.ToLower(getenv("KAFKA_TLS_ENABLED", "true")) == "true" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		caPath := getenv("KAFKA_TLS_CA_PATH", "")
		if caPath != "" {
			caPEM, err := os.ReadFile(caPath)
			if err != nil {
				return nil, fmt.Errorf("read kafka ca: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caPEM) {
				return nil, fmt.Errorf("invalid kafka ca")
			}
			tlsConfig.RootCAs = pool
		}
		cfg.Net.TLS.Enable = true
		cfg.Net.TLS.Config = tlsConfig
	}

	if strings.ToLower(getenv("KAFKA_SASL_ENABLED", "true")) == "true" {
		user := getenv("KAFKA_SASL_USER", "")
		if user == "" {
			user = getenv("KAFKA_SASL_USERNAME", "")
		}
		pass := getenv("KAFKA_SASL_PASSWORD", "")
		if user == "" || pass == "" {
			return nil, fmt.Errorf("missing sasl user/password")
		}
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.User = user
		cfg.Net.SASL.Password = pass
		mech := strings.ToLower(getenv("KAFKA_SASL_MECHANISM", "plain"))
		switch mech {
		case "scram-sha-256", "scram-sha-512":
			return nil, fmt.Errorf("scram not supported in probe (set KAFKA_SASL_MECHANISM=plain)")
		default:
			cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		}
	}

	return cfg, nil
}

func main() {
	brokers := splitBrokers()
	if len(brokers) == 0 {
		fmt.Println("missing KAFKA_BROKERS or KAFKA_BOOTSTRAP_SERVERS")
		os.Exit(2)
	}
	inputTopic := getenv("KAFKA_INPUT_TOPIC", "telemetry.flow.v1")
	outputTopic := getenv("KAFKA_OUTPUT_TOPIC", "telemetry.flow.agg.v1")

	cfg, err := buildConfig()
	if err != nil {
		fmt.Println("config error:", err)
		os.Exit(2)
	}

	client, err := sarama.NewClient(brokers, cfg)
	if err != nil {
		fmt.Println("client error:", err)
		os.Exit(2)
	}
	defer client.Close()

	partitions, err := client.Partitions(inputTopic)
	if err != nil || len(partitions) == 0 {
		fmt.Println("partition error:", err)
		os.Exit(2)
	}
	partition := partitions[0]

	producer, err := sarama.NewSyncProducerFromClient(client)
	if err != nil {
		fmt.Println("producer error:", err)
		os.Exit(2)
	}
	defer producer.Close()

	offset, _ := client.GetOffset(inputTopic, partition, sarama.OffsetNewest)
	msg := &sarama.ProducerMessage{
		Topic:     inputTopic,
		Partition: partition,
		Key:       sarama.StringEncoder("probe"),
		Value:     sarama.StringEncoder(`{"schema":"flow.v1","ts":0,"tenant_id":"probe","src_ip":"10.0.0.1","dst_ip":"10.0.0.2","src_port":1,"dst_port":2,"proto":6,"flow_id":"probe","bytes_fwd":1,"bytes_bwd":0,"pkts_fwd":1,"pkts_bwd":0,"duration_ms":1}`),
	}
	_, _, err = producer.SendMessage(msg)
	if err != nil {
		fmt.Println("produce error:", err)
		os.Exit(2)
	}

	consumer, err := sarama.NewConsumerFromClient(client)
	if err != nil {
		fmt.Println("consumer error:", err)
		os.Exit(2)
	}
	defer consumer.Close()

	pc, err := consumer.ConsumePartition(inputTopic, partition, offset)
	if err != nil {
		fmt.Println("consume partition error:", err)
		os.Exit(2)
	}
	defer pc.Close()

	select {
	case msg := <-pc.Messages():
		fmt.Printf("consume ok topic=%s partition=%d offset=%d\n", msg.Topic, msg.Partition, msg.Offset)
	case err := <-pc.Errors():
		fmt.Println("consume error:", err)
		os.Exit(2)
	case <-time.After(10 * time.Second):
		fmt.Println("consume timeout")
		os.Exit(2)
	}

	outMsg := &sarama.ProducerMessage{
		Topic:     outputTopic,
		Partition: partition,
		Key:       sarama.StringEncoder("probe"),
		Value:     sarama.StringEncoder(`{"schema":"flow.v1","ts":0,"tenant_id":"probe","src_ip":"10.0.0.1","dst_ip":"10.0.0.2","src_port":1,"dst_port":2,"proto":6,"flow_id":"probe-agg","bytes_fwd":1,"bytes_bwd":0,"pkts_fwd":1,"pkts_bwd":0,"duration_ms":1}`),
	}
	_, _, err = producer.SendMessage(outMsg)
	if err != nil {
		fmt.Println("produce output error:", err)
		os.Exit(2)
	}

	fmt.Println("probe ok")
}
