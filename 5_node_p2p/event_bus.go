package node_p2p

import (
	"context"
	"errors"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

/**
 * @file event_bus.go
 * @brief Hệ thống PubSub nội bộ để decoupling các module Mempool, SyncEngine và Network.
 * @details Sử dụng Watermill GoChannel để truyền tin nhắn nhanh trong bộ nhớ.
 */

var (
	GlobalEventBus *gochannel.GoChannel
	Logger         = watermill.NewStdLogger(false, false)
)

const (
	TopicBlockMined    = "block.mined"
	TopicBlockReceived = "block.received"
	TopicReorgDetected = "reorg.detected"
	TopicTxReceived    = "tx.received"
)

func InitEventBus() {
	GlobalEventBus = gochannel.NewGoChannel(
		gochannel.Config{
			BlockPublishUntilSubscriberAck: false,
		},
		Logger,
	)
}

// PublishEvent gửi một tin nhắn vào Event Bus
func PublishEvent(topic string, payload []byte) error {
	if GlobalEventBus == nil {
		return nil
	}
	msg := message.NewMessage(watermill.NewUUID(), payload)
	return GlobalEventBus.Publish(topic, msg)
}

// SubscribeEvent đăng ký lắng nghe một topic
func SubscribeEvent(ctx context.Context, topic string) (<-chan *message.Message, error) {
	if GlobalEventBus == nil {
		return nil, errors.New("event bus is not initialized")
	}
	return GlobalEventBus.Subscribe(ctx, topic)
}
