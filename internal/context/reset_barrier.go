package context

import "github.com/longyisang/emoagent/internal/storage"

func FilterHistoryAfterResetBarrier(history []storage.MessageRecord, barrier *ContextResetBarrier) []storage.MessageRecord {
	if barrier == nil || barrier.AfterMessageID == "" {
		return history
	}
	for i := range history {
		if history[i].ID == barrier.AfterMessageID {
			return append([]storage.MessageRecord(nil), history[i+1:]...)
		}
	}
	return nil
}
