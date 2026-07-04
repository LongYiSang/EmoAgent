package app

import "github.com/longyisang/emoagent/internal/platform"

type PlatformService struct {
	gateway *PlatformGateway
	manager *platform.Manager
}

func NewPlatformService(infra *Infra, conversation *ConversationService, commands *CommandService, chat *ChatService, personas *PersonaService) *PlatformService {
	var receipts platform.ReceiptStore
	if infra != nil && infra.DB != nil {
		receipts = NewStorageReceiptStore(infra.DB)
	}
	return &PlatformService{
		gateway: NewPlatformGateway(infra, conversation, commands, chat, personas, receipts),
		manager: platform.NewManager(),
	}
}

func (s *PlatformService) Gateway() *PlatformGateway {
	if s == nil {
		return nil
	}
	return s.gateway
}

func (s *PlatformService) Manager() *platform.Manager {
	if s == nil {
		return nil
	}
	return s.manager
}
