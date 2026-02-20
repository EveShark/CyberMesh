package kafka

import (
	"strings"

	"github.com/IBM/sarama"
	"github.com/xdg-go/scram"
)

type scramClient struct {
	*scram.Client
	*scram.ClientConversation
	hashGenerator scram.HashGeneratorFcn
}

func (s *scramClient) Begin(userName, password, authzID string) error {
	client, err := s.hashGenerator.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	s.Client = client
	s.ClientConversation = client.NewConversation()
	return nil
}

func (s *scramClient) Step(challenge string) (string, error) {
	return s.ClientConversation.Step(challenge)
}

func (s *scramClient) Done() bool {
	return s.ClientConversation.Done()
}

func scramClientGenerator(mechanism sarama.SASLMechanism) func() sarama.SCRAMClient {
	return func() sarama.SCRAMClient {
		switch strings.ToLower(string(mechanism)) {
		case strings.ToLower(string(sarama.SASLTypeSCRAMSHA512)):
			return &scramClient{hashGenerator: scram.SHA512}
		default:
			return &scramClient{hashGenerator: scram.SHA256}
		}
	}
}
