package context

type Mode string
type EmotionSlot string

const (
	ModeEmotion Mode = "emotion"
	ModeWork    Mode = "work"

	SlotPinnedContext EmotionSlot = "PinnedContext"
	SlotToolDigest    EmotionSlot = "ToolDigest"
	SlotRecentTurns   EmotionSlot = "RecentTurns"
)

var EmotionSlotOrder = []EmotionSlot{
	SlotPinnedContext,
	SlotToolDigest,
	SlotRecentTurns,
}
