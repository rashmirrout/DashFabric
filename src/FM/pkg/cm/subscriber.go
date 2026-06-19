package configmanagement

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdSubscriber implements Subscriber using etcd watch
type EtcdSubscriber struct {
	client  *clientv3.Client
	config  *SubscriptionConfig
	eventCh chan Event
	stopCh  chan struct{}
}

// NewEtcdSubscriber creates a new etcd-based subscriber
func NewEtcdSubscriber(cfg *SubscriptionConfig) (*EtcdSubscriber, error) {
	if cfg == nil {
		return nil, fmt.Errorf("subscription config required")
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{cfg.ControlBrokerAddr},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	return &EtcdSubscriber{
		client:  client,
		config:  cfg,
		eventCh: make(chan Event, 100),
		stopCh:  make(chan struct{}),
	}, nil
}

// Subscribe watches etcd topics and forwards events
func (s *EtcdSubscriber) Subscribe(ctx context.Context, topics []string) (<-chan Event, error) {
	if len(topics) == 0 {
		return nil, fmt.Errorf("at least one topic required")
	}

	// Start watching each topic
	for _, topic := range topics {
		go s.watchTopic(ctx, topic)
	}

	return s.eventCh, nil
}

// watchTopic monitors a single etcd key for changes
func (s *EtcdSubscriber) watchTopic(ctx context.Context, topic string) {
	watchCh := s.client.Watch(ctx, topic, clientv3.WithPrefix())

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case resp := <-watchCh:
			if resp.Err() != nil {
				continue
			}

			for _, event := range resp.Events {
				// Parse event from etcd value
				evt := Event{
					ID:        string(event.Kv.Key),
					Timestamp: time.Now(),
					Type:      "CB-Event",
				}
				evt.Fingerprint = evt.ComputeFingerprint()

				select {
				case s.eventCh <- evt:
				case <-s.stopCh:
					return
				}
			}
		}
	}
}

// Close closes the subscriber and etcd connection
func (s *EtcdSubscriber) Close() error {
	close(s.stopCh)
	return s.client.Close()
}
