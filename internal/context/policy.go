package context

type Mode string
type EmotionSlot string

const (
	ModeEmotion Mode = "emotion"
	ModeWork    Mode = "work"

	SlotPinnedContext  EmotionSlot = "PinnedContext"
	SlotRunningSummary EmotionSlot = "RunningSummary"
	SlotToolDigest     EmotionSlot = "ToolDigest"
	SlotRecentTurns    EmotionSlot = "RecentTurns"
)

var EmotionSlotOrder = []EmotionSlot{
	SlotPinnedContext,
	SlotRunningSummary,
	SlotToolDigest,
	SlotRecentTurns,
}
