/**
 * @file gossip_sub_handler.go
 * @brief Xử lý phát sóng và nhận dữ liệu qua Gossipsub.
 * @details Đảm bảo thông tin về Khối mới, Giao dịch chuẩn và Giao dịch gia cố 
 * được lan truyền nhanh chóng và đáng tin cậy trong mạng lưới.
 * 
 * @author Vô Nhật Thiên (Khởi tạo)
 * @date 2026-03-18
 */

package node_p2p

import (
	"context"
	"log"
	"github.com/libp2p/go-libp2p/core/peer"
)

// GossipHandler xử lý logic tin nhắn Gossip
type GossipHandler struct {
	Manager *NetworkManager
}

// Broadcast phát sóng dữ liệu đến một chủ đề cụ thể
func (g *GossipHandler) Broadcast(topicName string, data []byte) error {
	topic, ok := g.Manager.TopicMap[topicName]
	if !ok {
		var err error
		topic, err = g.Manager.JoinTopic(topicName)
		if err != nil {
			return err
		}
	}

	return topic.Publish(context.Background(), data)
}

// Subscribe lắng nghe dữ liệu từ một chủ đề
func (g *GossipHandler) Subscribe(topicName string, handler func(peer.ID, []byte)) {
	topic, ok := g.Manager.TopicMap[topicName]
	if !ok {
		var err error
		topic, err = g.Manager.JoinTopic(topicName)
		if err != nil {
			log.Printf("[ERROR] Không thể tham gia topic %s: %v", topicName, err)
			return
		}
	}

	sub, err := topic.Subscribe()
	if err != nil {
		log.Printf("[ERROR] Lỗi đăng ký topic %s: %v", topicName, err)
		return
	}

	go func() {
		for {
			msg, err := sub.Next(g.Manager.Ctx)
			if err != nil {
				log.Printf("[ERROR] Lỗi nhận tin nhắn Gossip: %v", err)
				return
			}
			// Chỉ xử lý tin nhắn từ peer khác
			if msg.ReceivedFrom == g.Manager.Host.ID() {
				continue
			}
			handler(msg.ReceivedFrom, msg.Data)
		}
	}()
}
