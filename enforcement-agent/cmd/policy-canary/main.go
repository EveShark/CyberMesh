package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"

	pb "backend/proto"
	"google.golang.org/protobuf/proto"
)

func main() {
	var (
		brokers  = flag.String("brokers", "127.0.0.1:9092", "kafka brokers (comma-separated)")
		topic    = flag.String("topic", "control.enforcement_command.v1", "policy topic")
		ackTopic = flag.String("ack-topic", "control.enforcement_ack.v1", "ack topic")
		waitAck  = flag.Bool("wait-ack", true, "wait for an ACK for this policy id")
		wait     = flag.Duration("wait", 30*time.Second, "max time to wait for ACK")

		privateKeyPath = flag.String("private-key", "", "path to Ed25519 private key PEM (PKCS8)")
		trustOutDir    = flag.String("trust-out", "", "optional dir to write signer public key PEM for the agent trust store")

		policyID  = flag.String("policy-id", "canary-1", "policy id (also used as Kafka key)")
		eventAction = flag.String("event-action", "add", "event action: add|approve")
		action    = flag.String("action", "drop", "policy action in rule_data: drop|reject")
		target    = flag.String("target", "10.0.0.1", "IP or CIDR to block")
		tenant    = flag.String("tenant", "default", "tenant identifier stamped into the rule payload")
		workflowID = flag.String("workflow-id", "", "workflow identifier stamped into payload/metadata/trace")
		requestID = flag.String("request-id", "", "request identifier stamped into payload/metadata/trace")
		commandID = flag.String("command-id", "", "command identifier stamped into payload/metadata/trace")
		namespace = flag.String("namespace", "cybermesh", "target namespace selector (for namespaced mode)")
		direction = flag.String("direction", "ingress", "ingress|egress|both")
		dryRun    = flag.Bool("dry-run", true, "set guardrails.dry_run in rule_data")
		ttl       = flag.Int("ttl-seconds", 60, "set guardrails.ttl_seconds in rule_data")
		approvalRequired = flag.Bool("approval-required", false, "set guardrails.approval_required in rule_data")
		preConsensusTTL = flag.Int("pre-consensus-ttl-seconds", 0, "set guardrails.pre_consensus_ttl_seconds in rule_data")
		rollbackIfNoCommitAfter = flag.Int("rollback-if-no-commit-after-s", 0, "set guardrails.rollback_if_no_commit_after_s in rule_data")
		allowlistIPs = flag.String("allowlist-ips", "", "comma-separated guardrails.allowlist.ips entries")
		allowlistCIDRs = flag.String("allowlist-cidrs", "", "comma-separated guardrails.allowlist.cidrs entries")
		allowlistNamespaces = flag.String("allowlist-namespaces", "", "comma-separated guardrails.allowlist.namespaces entries")
		timestampUnix = flag.Int64("timestamp-unix", 0, "override envelope timestamp (unix seconds)")
		selectors = flag.String("selectors", "", "comma-separated selectors for endpointSelector (e.g. app=cm-client,team=prod)")
		ports     = flag.String("ports", "", "comma-separated ports or ranges (e.g. 80 or 80-81)")
		protocols = flag.String("protocols", "TCP", "comma-separated protocols (e.g. TCP,UDP)")

		kafkaTLSEnabled  = flag.Bool("kafka-tls-enabled", false, "enable TLS for Kafka connection")
		kafkaSASLEnabled = flag.Bool("kafka-sasl-enabled", false, "enable SASL auth for Kafka connection")
		kafkaSASLUser    = flag.String("kafka-sasl-username", "", "Kafka SASL username (PLAIN/SCRAM)")
		kafkaSASLPass    = flag.String("kafka-sasl-password", "", "Kafka SASL password (PLAIN/SCRAM)")
		kafkaSASLMech    = flag.String("kafka-sasl-mechanism", "PLAIN", "Kafka SASL mechanism: PLAIN|SCRAM-SHA-256|SCRAM-SHA-512")
	)
	flag.Parse()

	if strings.TrimSpace(*privateKeyPath) == "" {
		fmt.Fprintln(os.Stderr, "policy-canary: --private-key is required")
		os.Exit(2)
	}

	priv, pub, err := loadEd25519Keypair(*privateKeyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "policy-canary: load key: %v\n", err)
		os.Exit(3)
	}

	if *trustOutDir != "" {
		if err := writePublicKeyPEM(*trustOutDir, "validator-local_public.pem", pub); err != nil {
			fmt.Fprintf(os.Stderr, "policy-canary: write trust key: %v\n", err)
			os.Exit(4)
		}
	}

	cfg := sarama.NewConfig()
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 3
	cfg.Producer.Return.Successes = true
	cfg.Version = sarama.V2_1_0_0
	cfg.Net.TLS.Enable = *kafkaTLSEnabled
	if *kafkaTLSEnabled {
		cfg.Net.TLS.Config = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	cfg.Net.SASL.Enable = *kafkaSASLEnabled
	if *kafkaSASLEnabled {
		cfg.Net.SASL.User = *kafkaSASLUser
		cfg.Net.SASL.Password = *kafkaSASLPass
		switch strings.ToUpper(strings.TrimSpace(*kafkaSASLMech)) {
		case "PLAIN":
			cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
		case "SCRAM-SHA-256":
			cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		case "SCRAM-SHA-512":
			cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		default:
			fmt.Fprintf(os.Stderr, "policy-canary: unsupported sasl mechanism %q\n", *kafkaSASLMech)
			os.Exit(11)
		}
	}

	bs := splitCSV(*brokers)
	if len(bs) == 0 {
		fmt.Fprintln(os.Stderr, "policy-canary: brokers empty")
		os.Exit(5)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *wait)
	defer cancel()

	var (
		ackErr error
		wg     sync.WaitGroup
	)
	if *waitAck {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ackErr = waitForAck(ctx, cfg, bs, *ackTopic, *policyID)
		}()
		// Give the consumer a moment to join and set offsets to newest.
		time.Sleep(1 * time.Second)
	}

	event, err := buildSignedPolicyEvent(priv, pub, buildPolicyOptions{
		PolicyID:   *policyID,
		Action:     *action,
		EventAction: *eventAction,
		Target:     *target,
		Tenant:     *tenant,
		WorkflowID: *workflowID,
		RequestID:  *requestID,
		CommandID:  *commandID,
		Namespace:  *namespace,
		Direction:  *direction,
		DryRun:     *dryRun,
		TTLSeconds: *ttl,
		ApprovalRequired: *approvalRequired,
		PreConsensusTTLSeconds: *preConsensusTTL,
		RollbackIfNoCommitAfter: *rollbackIfNoCommitAfter,
		AllowlistIPs: *allowlistIPs,
		AllowlistCIDRs: *allowlistCIDRs,
		AllowlistNamespaces: *allowlistNamespaces,
		TimestampUnix: *timestampUnix,
		Selectors:  *selectors,
		Ports:      *ports,
		Protocols:  *protocols,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "policy-canary: build event: %v\n", err)
		os.Exit(6)
	}

	encoded, err := proto.Marshal(event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "policy-canary: marshal event: %v\n", err)
		os.Exit(7)
	}

	producer, err := sarama.NewSyncProducer(bs, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "policy-canary: producer: %v\n", err)
		os.Exit(8)
	}
	defer producer.Close()

	msg := &sarama.ProducerMessage{
		Topic: *topic,
		Key:   sarama.StringEncoder(*policyID),
		Value: sarama.ByteEncoder(encoded),
	}
	if _, _, err := producer.SendMessage(msg); err != nil {
		fmt.Fprintf(os.Stderr, "policy-canary: send: %v\n", err)
		os.Exit(9)
	}
	fmt.Printf("policy-canary: published policy_id=%s topic=%s\n", *policyID, *topic)

	if *waitAck {
		wg.Wait()
		if ackErr != nil {
			fmt.Fprintf(os.Stderr, "policy-canary: ack wait failed: %v\n", ackErr)
			os.Exit(10)
		}
		fmt.Printf("policy-canary: ack received policy_id=%s topic=%s\n", *policyID, *ackTopic)
	}
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func loadEd25519Keypair(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, nil, errors.New("invalid pem")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	priv, ok := keyAny.(ed25519.PrivateKey)
	if !ok {
		return nil, nil, errors.New("not ed25519 private key")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, nil, errors.New("invalid ed25519 private key size")
	}
	pub := priv.Public().(ed25519.PublicKey)
	return priv, pub, nil
}

func writePublicKeyPEM(dir, name string, pub ed25519.PublicKey) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

type buildPolicyOptions struct {
	PolicyID   string
	Action     string
	EventAction string
	Target     string
	Tenant     string
	WorkflowID string
	RequestID  string
	CommandID  string
	Namespace  string
	Direction  string
	DryRun     bool
	TTLSeconds int
	ApprovalRequired bool
	PreConsensusTTLSeconds int
	RollbackIfNoCommitAfter int
	AllowlistIPs string
	AllowlistCIDRs string
	AllowlistNamespaces string
	TimestampUnix int64
	Selectors  string
	Ports      string
	Protocols  string
}

func buildSignedPolicyEvent(priv ed25519.PrivateKey, pub ed25519.PublicKey, opts buildPolicyOptions) (*pb.PolicyUpdateEvent, error) {
	now := time.Now().UTC()
	if opts.TimestampUnix > 0 {
		now = time.Unix(opts.TimestampUnix, 0).UTC()
	}

	dir := strings.ToLower(strings.TrimSpace(opts.Direction))
	if dir == "both" {
		dir = ""
	}
	if dir != "" && dir != "ingress" && dir != "egress" {
		return nil, fmt.Errorf("invalid direction %q", opts.Direction)
	}

	act := strings.ToLower(strings.TrimSpace(opts.Action))
	if act == "" {
		act = "drop"
	}
	if act != "drop" && act != "reject" {
		return nil, fmt.Errorf("invalid action %q (expected drop|reject)", opts.Action)
	}
	eventAction := strings.ToLower(strings.TrimSpace(opts.EventAction))
	if eventAction == "" {
		eventAction = "add"
	}
	if eventAction != "add" && eventAction != "approve" {
		return nil, fmt.Errorf("invalid event action %q (expected add|approve)", opts.EventAction)
	}

	// rule_data is the source of truth for the agent. It must match policy.ParseSpec expectations.
	rule := map[string]any{
		"policy_id": opts.PolicyID,
		"rule_type": "block",
		"action":    act,
		"tenant":    strings.TrimSpace(opts.Tenant),
		"target": map[string]any{
			"direction": dir,
			"namespace": strings.ToLower(strings.TrimSpace(opts.Namespace)),
			"tenant":    strings.TrimSpace(opts.Tenant),
		},
		"guardrails": map[string]any{
			"dry_run":     opts.DryRun,
			"ttl_seconds": normalizeTTLSeconds(opts.TTLSeconds),
		},
	}
	if workflowID := strings.TrimSpace(opts.WorkflowID); workflowID != "" {
		rule["workflow_id"] = workflowID
	}
	metadata := map[string]any{}
	trace := map[string]any{}
	if requestID := strings.TrimSpace(opts.RequestID); requestID != "" {
		rule["request_id"] = requestID
		metadata["request_id"] = requestID
		trace["request_id"] = requestID
	}
	if commandID := strings.TrimSpace(opts.CommandID); commandID != "" {
		rule["command_id"] = commandID
		metadata["command_id"] = commandID
		trace["command_id"] = commandID
	}
	if workflowID := strings.TrimSpace(opts.WorkflowID); workflowID != "" {
		metadata["workflow_id"] = workflowID
		trace["workflow_id"] = workflowID
	}
	if tenant := strings.TrimSpace(opts.Tenant); tenant != "" {
		metadata["tenant"] = tenant
		trace["tenant"] = tenant
	}
	if len(metadata) > 0 {
		rule["metadata"] = metadata
	}
	if len(trace) > 0 {
		rule["trace"] = trace
	}
	if opts.ApprovalRequired {
		rule["guardrails"].(map[string]any)["approval_required"] = true
	}
	if opts.PreConsensusTTLSeconds > 0 {
		rule["guardrails"].(map[string]any)["pre_consensus_ttl_seconds"] = opts.PreConsensusTTLSeconds
	}
	if opts.RollbackIfNoCommitAfter > 0 {
		rule["guardrails"].(map[string]any)["rollback_if_no_commit_after_s"] = opts.RollbackIfNoCommitAfter
	}
	t := strings.TrimSpace(opts.Target)
	if t == "" {
		return nil, errors.New("target empty")
	}
	if strings.Contains(t, "/") {
		rule["target"].(map[string]any)["cidrs"] = []string{t}
	} else {
		rule["target"].(map[string]any)["ips"] = []string{t}
	}

	if sel := parseSelectors(opts.Selectors); len(sel) > 0 {
		rule["target"].(map[string]any)["selectors"] = sel
	}
	if pr := parsePortsFlag(opts.Ports); len(pr) > 0 {
		rule["target"].(map[string]any)["ports"] = pr
	}
	if protos := splitCSV(opts.Protocols); len(protos) > 0 {
		rule["target"].(map[string]any)["protocols"] = protos
	}
	if allowlist := buildAllowlist(opts.AllowlistIPs, opts.AllowlistCIDRs, opts.AllowlistNamespaces); allowlist != nil {
		rule["guardrails"].(map[string]any)["allowlist"] = allowlist
	}

	ruleBytes, err := json.Marshal(rule)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(ruleBytes)

	evt := &pb.PolicyUpdateEvent{
		PolicyId:         opts.PolicyID,
		Action:           eventAction,
		RuleType:         "block",
		RuleData:         ruleBytes,
		RuleHash:         hash[:],
		RequiresAck:      true,
		RollbackPolicyId: "",
		Timestamp:        now.Unix(),
		EffectiveHeight:  0,
		ExpirationHeight: 0,
		ProducerId:       append([]byte(nil), pub...),
		Pubkey:           append([]byte(nil), pub...),
		Alg:              "Ed25519",
	}

	// Signature must match enforcement-agent VerifyAndParse domain separation.
	signed := pb.PolicyUpdateEvent{
		PolicyId:         evt.PolicyId,
		Action:           evt.Action,
		RuleType:         evt.RuleType,
		RuleData:         evt.RuleData,
		RuleHash:         evt.RuleHash,
		RequiresAck:      evt.RequiresAck,
		RollbackPolicyId: evt.RollbackPolicyId,
		Timestamp:        evt.Timestamp,
		EffectiveHeight:  evt.EffectiveHeight,
		ExpirationHeight: evt.ExpirationHeight,
		ProducerId:       evt.ProducerId,
	}
	raw, err := proto.Marshal(&signed)
	if err != nil {
		return nil, err
	}
	msg := append([]byte("control.policy.v2"), raw...)
	evt.Signature = ed25519.Sign(priv, msg)
	return evt, nil
}

func buildAllowlist(ipsCSV, cidrsCSV, namespacesCSV string) map[string]any {
	ips := splitCSV(ipsCSV)
	cidrs := splitCSV(cidrsCSV)
	namespaces := splitCSV(namespacesCSV)
	if len(ips) == 0 && len(cidrs) == 0 && len(namespaces) == 0 {
		return nil
	}
	return map[string]any{
		"ips":        ips,
		"cidrs":      cidrs,
		"namespaces": namespaces,
	}
}

func normalizeTTLSeconds(ttl int) int {
	// The agent requires a positive integer ttl_seconds for guardrails.
	if ttl <= 0 {
		return 60
	}
	return ttl
}

func parseSelectors(raw string) map[string]any {
	out := map[string]any{}
	for _, entry := range splitCSV(raw) {
		kv := strings.SplitN(entry, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parsePortsFlag(raw string) []any {
	var out []any
	for _, entry := range splitCSV(raw) {
		s := strings.TrimSpace(entry)
		if s == "" {
			continue
		}
		if strings.Contains(s, "-") {
			parts := strings.SplitN(s, "-", 2)
			if len(parts) != 2 {
				continue
			}
			from, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			to, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err1 != nil || err2 != nil {
				continue
			}
			out = append(out, map[string]any{
				"from": from,
				"to":   to,
			})
			continue
		}
		p, err := strconv.Atoi(s)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func waitForAck(ctx context.Context, cfg *sarama.Config, brokers []string, topic, policyID string) error {
	groupID := fmt.Sprintf("policy-canary-%d", time.Now().UnixNano())
	cCfg := *cfg
	cCfg.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRange()
	cCfg.Consumer.Offsets.Initial = sarama.OffsetNewest

	group, err := sarama.NewConsumerGroup(brokers, groupID, &cCfg)
	if err != nil {
		return err
	}
	defer group.Close()

	handler := &ackHandler{policyID: policyID, done: make(chan struct{})}
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			_ = group.Consume(ctx, []string{topic}, handler)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-handler.done:
		return handler.err
	}
}

type ackHandler struct {
	policyID string
	done     chan struct{}
	once     sync.Once
	err      error
}

func (h *ackHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *ackHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *ackHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		var ack pb.PolicyAckEvent
		if err := proto.Unmarshal(msg.Value, &ack); err != nil {
			// Ignore malformed ACKs; this is a canary watcher.
			sess.MarkMessage(msg, "")
			continue
		}
		if ack.PolicyId == h.policyID {
			h.once.Do(func() {
				close(h.done)
			})
			sess.MarkMessage(msg, "")
			return nil
		}
		sess.MarkMessage(msg, "")
	}
	return nil
}
